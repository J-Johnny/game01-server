package mongodb

import (
	"context"
	"errors"
	"testing"
)

func TestMongoUnitOfWorkExecutesAndEndsSession(t *testing.T) {
	factory := &fakeSessionFactory{session: &fakeTransactionSession{}}
	unit := NewMongoUnitOfWorkWithFactory(factory)
	called := false
	if err := unit.Execute(context.Background(), func(ctx context.Context) error {
		called = ctx != nil
		return nil
	}); err != nil {
		t.Fatalf("execute transaction: %v", err)
	}
	if !called || !factory.session.withTransactionCalled || !factory.session.endSessionCalled {
		t.Fatalf("transaction lifecycle was not completed: %+v", factory.session)
	}
}

func TestMongoUnitOfWorkPropagatesCallbackError(t *testing.T) {
	factory := &fakeSessionFactory{session: &fakeTransactionSession{}}
	unit := NewMongoUnitOfWorkWithFactory(factory)
	wantErr := errors.New("rollback callback error")
	if err := unit.Execute(context.Background(), func(context.Context) error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("execute error = %v, want %v", err, wantErr)
	}
	if !factory.session.endSessionCalled {
		t.Fatal("session was not ended after callback error")
	}
}

func TestMongoUnitOfWorkRejectsInvalidInput(t *testing.T) {
	if err := (*MongoUnitOfWork)(nil).Execute(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrMongoClientRequired) {
		t.Fatalf("nil unit error = %v", err)
	}
	unit := NewMongoUnitOfWorkWithFactory(&fakeSessionFactory{session: &fakeTransactionSession{}})
	if err := unit.Execute(context.Background(), nil); !errors.Is(err, ErrTransactionFuncNil) {
		t.Fatalf("nil function error = %v", err)
	}
}

type fakeSessionFactory struct {
	session *fakeTransactionSession
}

func (f *fakeSessionFactory) Start(context.Context) (TransactionSession, error) {
	return f.session, nil
}

type fakeTransactionSession struct {
	withTransactionCalled bool
	endSessionCalled      bool
}

func (s *fakeTransactionSession) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	s.withTransactionCalled = true
	return fn(ctx)
}

func (s *fakeTransactionSession) EndSession(context.Context) {
	s.endSessionCalled = true
}
