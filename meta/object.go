package meta

import (
	"database/sql"

	. "github.com/journeymidnight/yig/context"
	. "github.com/journeymidnight/yig/error"
	"github.com/journeymidnight/yig/helper"
	. "github.com/journeymidnight/yig/meta/types"
	"github.com/journeymidnight/yig/redis"
)

func (m *Meta) GetObject(bucketName string, objectName string, willNeed bool) (object *Object, err error) {
	getObject := func() (o interface{}, err error) {
		helper.Logger.Info("GetObject CacheMiss. bucket:", bucketName,
			"object:", objectName)
		object, err := m.Client.GetObject(bucketName, objectName, "")
		if err != nil {
			return
		}
		helper.Logger.Info("GetObject object.Name:", object.Name)
		if object.Name != objectName {
			err = ErrNoSuchKey
			return
		}
		return object, nil
	}
	unmarshaller := func(in []byte) (interface{}, error) {
		var object Object
		err := helper.MsgPackUnMarshal(in, &object)
		return &object, err
	}

	o, err := m.Cache.Get(redis.ObjectTable, bucketName+":"+objectName+":",
		getObject, unmarshaller, willNeed)
	if err != nil {
		return
	}
	object, ok := o.(*Object)
	if !ok {
		err = ErrInternalError
		return
	}
	return object, nil
}

func (m *Meta) GetAllObject(bucketName string, objectName string) (object []*Object, err error) {
	return m.Client.GetAllObject(bucketName, objectName, "")
}

func (m *Meta) GetAllOldObjects(bucketName string, objectName string, latestVersion string) (objects []*Object, err error) {
	return m.Client.GetAllOldObjects(bucketName, objectName, latestVersion)
}

func (m *Meta) GetObjectMap(bucketName, objectName string) (objMap *ObjMap, err error) {
	m.Client.GetObjectMap(bucketName, objectName)
	return
}

func (m *Meta) GetObjectVersion(bucketName, objectName, version string, willNeed bool) (object *Object, err error) {
	getObjectVersion := func() (o interface{}, err error) {
		object, err := m.Client.GetObject(bucketName, objectName, version)
		if err != nil {
			return
		}
		if object.Name != objectName {
			err = ErrNoSuchKey
			return
		}
		return object, nil
	}
	unmarshaller := func(in []byte) (interface{}, error) {
		var object Object
		err := helper.MsgPackUnMarshal(in, &object)
		return &object, err
	}
	o, err := m.Cache.Get(redis.ObjectTable, bucketName+":"+objectName+":"+version,
		getObjectVersion, unmarshaller, willNeed)
	if err != nil {
		return
	}
	object, ok := o.(*Object)
	if !ok {
		err = ErrInternalError
		return
	}
	return object, nil
}

func (m *Meta) PutObject(reqCtx RequestContext, object *Object, multipart *Multipart, objMap *ObjMap, updateUsage bool) error {
	if reqCtx.BucketInfo == nil {
		return ErrNoSuchBucket
	}
	if reqCtx.BucketInfo.Versioning == VersionDisabled {
		object.VersionId = "0"
	} else {
		return ErrNotImplemented
		// TODO: object.VersionId = strconv.FormatUint(math.MaxUint64-uint64(object.LastModifiedTime.UnixNano()), 10)
	}

	needUpdate := (reqCtx.ObjectInfo != nil)
	if multipart == nil && object.Parts == nil {
		if needUpdate {
			return m.Client.UpdateObjectWithoutMultiPart(object)
		} else {
			return m.Client.PutObjectWithoutMultiPart(object)
		}

	}

	tx, err := m.Client.NewTrans()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			m.Client.AbortTrans(tx)
		}
	}()

	if needUpdate {
		err = m.Client.UpdateObject(object, tx)
		if err != nil {
			return err
		}
	} else {
		err = m.Client.PutObject(object, tx)
		if err != nil {
			return err
		}
	}

	if multipart != nil {
		err = m.Client.DeleteMultipart(multipart, tx)
		if err != nil {
			return err
		}
	}

	if updateUsage {
		err = m.Client.UpdateUsage(object.BucketName, object.Size, tx)
		if err != nil {
			return err
		}
	}
	return m.Client.CommitTrans(tx)
}

func (m *Meta) PutObjectEntry(object *Object) error {
	err := m.Client.PutObject(object, nil)
	return err
}

func (m *Meta) UpdateObjectAcl(object *Object) error {
	err := m.Client.UpdateObjectAcl(object)
	return err
}

func (m *Meta) UpdateObjectAttrs(object *Object) error {
	err := m.Client.UpdateObjectAttrs(object)
	return err
}

func (m *Meta) RenameObject(object *Object, sourceObject string) error {
	err := m.Client.RenameObject(object, sourceObject, nil)
	return err
}

func (m *Meta) PutObjMapEntry(objMap *ObjMap) error {
	err := m.Client.PutObjectMap(objMap, nil)
	return err
}

func (m *Meta) DeleteOldObject(object *Object) (err error) {
	var tx *sql.Tx
	tx, err = m.Client.NewTrans()
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			err = m.Client.CommitTrans(tx)
		}
		if err != nil {
			m.Client.AbortTrans(tx)
		}
	}()

	return m.Client.UpdateUsage(object.BucketName, -object.Size, tx)
}

func (m *Meta) DeleteObject(object *Object) (err error) {
	var tx *sql.Tx
	tx, err = m.Client.NewTrans()
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			err = m.Client.CommitTrans(tx)
		}
		if err != nil {
			m.Client.AbortTrans(tx)
		}
	}()

	err = m.Client.DeleteObject(object, tx)
	if err != nil {
		return err
	}

	err = m.Client.PutObjectToGarbageCollection(object, tx)
	if err != nil {
		return err
	}

	return m.Client.UpdateUsage(object.BucketName, -object.Size, tx)
}

func (m *Meta) AppendObject(object *Object, isExist bool) error {
	tx, err := m.Client.NewTrans()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			m.Client.AbortTrans(tx)
		}
	}()
	if !isExist {
		err = m.Client.PutObject(object, tx)
	} else {
		err = m.Client.UpdateAppendObject(object, tx)
	}
	if err != nil {
		return err
	}
	err = m.Client.UpdateUsage(object.BucketName, object.Size, tx)
	if err != nil {
		return err
	}
	return m.Client.CommitTrans(tx)
}
