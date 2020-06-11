package restore

import (
	"github.com/journeymidnight/yig-restore/redis"
	"os"
	"sync"
	"time"

	"github.com/bsm/redislock"
	"github.com/journeymidnight/yig-restore/helper"
)

var (
	ShutDown    chan bool
	WG          sync.WaitGroup
	mutexs      map[string]*redislock.Lock
	mux         sync.Mutex
	SignalQueue chan os.Signal
)

func autoRefreshLock() {
	WG.Add(1)
	c := time.Tick(time.Duration(helper.Conf.RefreshLockTime) * time.Minute)
	for {
		select {
		case <-c:
			for key, lock := range mutexs {
				err := lock.Refresh(time.Duration(helper.Conf.LockTime)*time.Minute, nil)
				if err != nil {
					if err == redislock.ErrNotObtained {
						helper.Logger.Info("No longer hold lock ...", key)
					} else {
						helper.Logger.Info("Refresh lock failed ...", key, err.Error())
					}
					mux.Lock()
					delete(mutexs, key)
					mux.Unlock()
					continue
				}
				helper.Logger.Info("Refresh lock success...", key)
			}
		case <-ShutDown:
			helper.Logger.Info("Shutting down, Release all locks")
			for key, _ := range mutexs {
				redis.Remove(key)
			}
			WG.Done()
		}
	}
}