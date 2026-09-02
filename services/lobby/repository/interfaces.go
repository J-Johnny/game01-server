package repository

import (
	"context"

	"server/services/lobby/domain"
)

type PlayerRepository interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, domain.Player) error
	FindDefaultByAccountID(context.Context, string) (*domain.Player, error)
	FindByID(context.Context, string) (*domain.Player, error)
}

type AssetRepository interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, domain.Assets) error
	FindByPlayerID(context.Context, string) (*domain.Assets, error)
	ApplyDelta(context.Context, string, string, int64) (*domain.Assets, error)
}

type LedgerRepository interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, domain.Settlement) error
	FindBySettlementID(context.Context, string) (*domain.Settlement, error)
}

type SnapshotRepository interface {
	EnsureIndexes(context.Context) error
	FindByPlayerID(context.Context, string) (*domain.Snapshot, error)
	Save(context.Context, domain.Snapshot) error
}
