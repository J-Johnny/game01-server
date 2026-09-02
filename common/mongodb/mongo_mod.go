package mongodb

import (
	"context"
	"fmt"

	"server/common/config"

	driverMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type IMongoModule interface {
	DriverResources
	Init() error
	AfterInit()
	BeforeShutdown()
	Shutdown() error
}

type DriverResources interface {
	DriverClient() *driverMongo.Client
	DriverDatabase() *driverMongo.Database
	DriverCollection(name string) *driverMongo.Collection
}

type MongoModule struct {
	dbConf       config.MongoConfig
	driverClient *driverMongo.Client
	driverDB     *driverMongo.Database
}

func NewMongoModule(conf *config.Config) *MongoModule {
	m := &MongoModule{
		dbConf: conf.Mongo,
	}
	return m
}

func (m *MongoModule) DriverClient() *driverMongo.Client {
	if m == nil {
		return nil
	}
	return m.driverClient
}

func (m *MongoModule) DriverDatabase() *driverMongo.Database {
	if m == nil {
		return nil
	}
	return m.driverDB
}

func (m *MongoModule) DriverCollection(name string) *driverMongo.Collection {
	if m == nil || m.driverDB == nil {
		return nil
	}
	return m.driverDB.Collection(name)
}

func (m *MongoModule) Init() error {
	driverOptions := options.Client().ApplyURI(m.dbConf.URI).SetConnectTimeout(m.dbConf.ConnectTimeout)
	if m.dbConf.Username != "" || m.dbConf.Password != "" {
		driverOptions.SetAuth(options.Credential{
			AuthMechanism: m.dbConf.AuthMechanism,
			AuthSource:    m.dbConf.AuthSource,
			Username:      m.dbConf.Username,
			Password:      m.dbConf.Password,
		})
	}
	driverClient, err := driverMongo.Connect(context.Background(), driverOptions)
	if err != nil {
		return fmt.Errorf("connect mongodb: %w", err)
	}
	pingContext, cancel := context.WithTimeout(context.Background(), m.dbConf.ConnectTimeout)
	err = driverClient.Ping(pingContext, readpref.Primary())
	cancel()
	if err != nil {
		_ = driverClient.Disconnect(context.Background())
		return fmt.Errorf("ping mongodb: %w", err)
	}
	m.driverClient = driverClient
	m.driverDB = driverClient.Database(m.dbConf.Database)
	return nil
}

func (m *MongoModule) AfterInit() {
}

func (m *MongoModule) BeforeShutdown() {
}

func (m *MongoModule) Shutdown() error {
	ctx := context.Background()
	var first error
	if m != nil && m.driverClient != nil {
		if err := m.driverClient.Disconnect(ctx); err != nil {
			first = err
		}
	}
	return first
}

var _ DriverResources = (*MongoModule)(nil)
