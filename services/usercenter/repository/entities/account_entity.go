package entities

import (
	"context"
	"server/services/usercenter/repository/models"
)

type AccountEntity struct {
	ctx     context.Context
	account *models.Account
}

func NewAccountEntity(ctx context.Context, account *models.Account) *AccountEntity {
	return &AccountEntity{
		ctx:     ctx,
		account: account,
	}
}
