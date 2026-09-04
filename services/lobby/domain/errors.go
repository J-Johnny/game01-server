package domain

import "errors"

var (
	ErrPlayerNotFound       = errors.New("player not found")
	ErrAssetNotFound        = errors.New("player assets not found")
	ErrInvalidPlayer        = errors.New("invalid player")
	ErrInvalidSettlement    = errors.New("invalid settlement")
	ErrSettlementNotFound   = errors.New("settlement not found")
	ErrSettlementApplied    = errors.New("settlement already applied")
	ErrSettlementConflict   = errors.New("settlement id conflict")
	ErrInsufficientCurrency = errors.New("insufficient currency")
)
