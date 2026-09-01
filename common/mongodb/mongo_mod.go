package mongodb

import (
	"context"
	"server/common/config"

	"github.com/pkg/errors"
	"github.com/qiniu/qmgo"
)

type IMongoModule interface {
	Database() *qmgo.Database
	Collection(name string) *qmgo.Collection
}

type MongoModule struct {
	dbConf   config.MongoConfig
	client   *qmgo.Client
	dbClient *qmgo.Database
}

func NewMongoModule(conf *config.Config) *MongoModule {
	m := &MongoModule{
		dbConf: conf.Mongo,
	}
	return m
}

func (m *MongoModule) Collection(name string) *qmgo.Collection {
	return m.dbClient.Collection(name)
}

func (m *MongoModule) Database() *qmgo.Database {
	return m.dbClient
}

func (m *MongoModule) Init() error {
	qmgoConfig := &qmgo.Config{Uri: m.dbConf.URI}
	if m.dbConf.Username != "" || m.dbConf.Password != "" {
		qmgoConfig.Auth = &qmgo.Credential{
			AuthMechanism: m.dbConf.AuthMechanism,
			AuthSource:    m.dbConf.AuthSource,
			Username:      m.dbConf.Username,
			Password:      m.dbConf.Password,
		}
	}
	cli, err := qmgo.NewClient(context.Background(), qmgoConfig)
	if err != nil {
		return errors.Wrap(err, "new mongodb client error")
	}
	m.client = cli
	err = m.client.Ping(5)
	if err != nil {
		return errors.Wrap(err, "ping mongodb error")
	}
	m.dbClient = m.client.Database(m.dbConf.Database)
	return nil
}

func (m *MongoModule) AfterInit() {
}

func (m *MongoModule) BeforeShutdown() {
}

func (m *MongoModule) Shutdown() error {
	return m.client.Close(context.Background())
}
