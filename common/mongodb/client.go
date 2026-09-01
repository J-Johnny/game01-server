package mongodb

import (
	"context"
	"fmt"

	"github.com/qiniu/qmgo"
	"server/common/config"
)

type Resources struct {
	Client   *qmgo.Client
	Database *qmgo.Database
}

func Open(ctx context.Context, cfg config.MongoConfig) (*Resources, error) {
	timeoutMS := cfg.ConnectTimeout.Milliseconds()
	client, err := qmgo.Open(ctx, &qmgo.Config{Uri: cfg.URI, Database: cfg.Database, ConnectTimeoutMS: &timeoutMS})
	if err != nil {
		return nil, fmt.Errorf("connect mongo: %w", err)
	}

	return &Resources{Client: client.Client, Database: client.Database}, nil
}

func (r *Resources) Close(ctx context.Context) error {
	if r == nil || r.Client == nil {
		return nil
	}

	return r.Client.Close(ctx)
}
