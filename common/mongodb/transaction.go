package mongodb

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/mongo"
)

var (
	ErrMongoClientRequired = errors.New("mongo client is required")
	ErrTransactionFuncNil  = errors.New("transaction function is required")
)

// UnitOfWork executes a group of persistence operations in one local transaction.
// Implementations own session lifecycle and transaction commit/abort behavior.
type UnitOfWork interface {
	Execute(context.Context, func(context.Context) error) error
}

type SessionFactory interface {
	Start(context.Context) (TransactionSession, error)
}

type TransactionSession interface {
	WithTransaction(context.Context, func(context.Context) error) error
	EndSession(context.Context)
}

type MongoUnitOfWork struct {
	sessions SessionFactory
}

func NewMongoUnitOfWork(client *mongo.Client) *MongoUnitOfWork {
	return &MongoUnitOfWork{
		sessions: newMongoSessionFactory(client),
	}
}

func NewMongoUnitOfWorkWithFactory(factory SessionFactory) *MongoUnitOfWork {
	return &MongoUnitOfWork{
		sessions: factory,
	}
}

func (u *MongoUnitOfWork) Execute(ctx context.Context, fn func(context.Context) error) error {
	if u == nil || u.sessions == nil {
		return ErrMongoClientRequired
	}
	if fn == nil {
		return ErrTransactionFuncNil
	}
	session, err := u.sessions.Start(ctx)
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	return session.WithTransaction(ctx, fn)
}

type mongoSessionFactory struct {
	client *mongo.Client
}

func newMongoSessionFactory(client *mongo.Client) *mongoSessionFactory {
	return &mongoSessionFactory{
		client: client,
	}
}

func (f *mongoSessionFactory) Start(_ context.Context) (TransactionSession, error) {
	if f == nil || f.client == nil {
		return nil, ErrMongoClientRequired
	}
	session, err := f.client.StartSession()
	if err != nil {
		return nil, err
	}
	return &mongoTransactionSession{
		session: session,
	}, nil
}

type mongoTransactionSession struct {
	session mongo.Session
}

func (s *mongoTransactionSession) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	if s == nil || s.session == nil {
		return ErrMongoClientRequired
	}
	_, err := s.session.WithTransaction(ctx, func(sessionContext mongo.SessionContext) (interface{}, error) {
		return nil, fn(sessionContext)
	})
	return err
}

func (s *mongoTransactionSession) EndSession(ctx context.Context) {
	if s != nil && s.session != nil {
		s.session.EndSession(ctx)
	}
}
