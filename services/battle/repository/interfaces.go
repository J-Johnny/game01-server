package repository

import (
	"context"

	"server/services/battle/domain"
)

type RoomSnapshotRepository interface {
	EnsureIndexes(context.Context) error
	Save(context.Context, *domain.Room) error
	FindByPlayerID(context.Context, string) (*domain.Room, error)
}
