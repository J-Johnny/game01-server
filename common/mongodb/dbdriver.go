package mongodb

import (
	"context"
	"server/common/util/mongoutil"

	"github.com/qiniu/qmgo"
	"go.mongodb.org/mongo-driver/bson"
)

type DBDriver struct {
	Client *qmgo.Collection
}

func NewDBDriver(collection *qmgo.Collection) *DBDriver {
	m := &DBDriver{Client: collection}
	return m
}

func (m *DBDriver) HasById(ctx context.Context, id interface{}) (ok bool, err error) {
	return m.Has(ctx, "_id", id)
}

func (m *DBDriver) InsertOne(ctx context.Context, data interface{}) error {
	_, err := m.Client.InsertOne(ctx, data)
	if err != nil {
		return err
	}
	return nil
}

func (m *DBDriver) UpdateId(ctx context.Context, id interface{}, data interface{}) error {
	err := m.Client.UpdateId(ctx, id, bson.M{"$set": data})
	if err != nil {
		return err
	}
	return nil
}

func (m *DBDriver) GetById(ctx context.Context, id interface{}, data interface{}) (ok bool, err error) {
	err = m.Client.Find(ctx, bson.M{"_id": id}).One(data)
	if err != nil {
		if mongoutil.IsNoDoc(err) {
			return false, nil
		} else {
			return false, err
		}
	}
	return true, nil
}

func (m *DBDriver) GetManyById(ctx context.Context, ids []string, data interface{}) (ok bool, err error) {
	err = m.Client.Find(ctx, bson.M{"_id": bson.M{"$in": ids}}).All(data)
	if err != nil {
		if mongoutil.IsNoDoc(err) {
			return false, nil
		} else {
			return false, err
		}
	}
	return true, nil
}

func (m *DBDriver) Has(ctx context.Context, filterKey string, value interface{}) (ok bool, err error) {
	var n int64
	n, err = m.Client.Find(ctx, bson.M{filterKey: value}).Count()
	if err != nil {
		return
	}
	if n == 0 {
		return false, nil
	}
	return true, nil
}

func (m *DBDriver) Apply(ctx context.Context, filter interface{}, update interface{}, result interface{}) error {
	return m.Client.Find(ctx, filter).Apply(qmgo.Change{Update: update, ReturnNew: true}, result)
}
