package repository

import (
	"context"
	"errors"
	"fmt"
	"server/common/mongodb"
	"server/common/util/panicutil"
	"server/services/common/repository/dbmodel/md"
	"time"

	"server/services/usercenter/repository/models"

	"github.com/qiniu/qmgo"
	"go.mongodb.org/mongo-driver/bson"
)

type IAccountRepository interface {
	EnsureIndexes(context.Context) error
	Create(context.Context, *models.Account) error
	FindByID(context.Context, string) (models.Account, error)
	FindByIdentity(context.Context, models.AuthProvider, string) (models.Account, error)
	FindByRefreshTokenHash(context.Context, string, time.Time) (models.Account, error)
	LinkPlayer(context.Context, string, string, time.Time) error
	StoreRefreshToken(context.Context, string, models.RefreshToken, time.Time) error
	RevokeRefreshToken(context.Context, string, string, time.Time) error
	RotateRefreshToken(context.Context, string, string, string, time.Time, models.RefreshToken) (models.Account, error)
}

type AccountRepository struct {
	roleSchema *md.ModelRootSchema
	driver     *mongodb.DBDriver
}

func NewAccountRepository(coll *qmgo.Collection) *AccountRepository {
	r := &AccountRepository{}
	roleSchema, err := md.GlobalSchemaManager.Parse(&models.Account{})
	panicutil.Must(err)
	r.roleSchema = roleSchema
	r.driver = mongodb.NewDBDriver(coll)
	return r
}

func (r *AccountRepository) EnsureIndexes(ctx context.Context) error {
	if r.driver == nil {
		return errors.New("mongo account repository driver is required")
	}

	return r.driver.Client.EnsureIndexes(ctx, []string{"account_id"}, nil)
}

func (r *AccountRepository) Create(ctx context.Context, account *models.Account) error {
	if account == nil {
		return models.ErrInvalidAccount
	}

	if r.driver == nil {
		return errors.New("mongo account repository driver is required")
	}

	err := account.SetDefault()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	account.Identities.Data().Rows.Foreach2(func(key int64, value *md.Node[*models.Identity]) (isBreak bool) {
		value.Data().LinkedAt = now
		return false
	})

	if err := md.Init2(account); err != nil {
		return fmt.Errorf("initialize account model: %w", err)
	}
	_, err = r.driver.Client.InsertOne(ctx, account)
	if qmgo.IsDup(err) {
		return fmt.Errorf("create account: identity or account id already exists: %w", err)
	}
	if err != nil {
		return fmt.Errorf("create account: %w", err)
	}

	return nil
}

func (r *AccountRepository) FindByID(ctx context.Context, accountID string) (models.Account, error) {
	return r.findOne(ctx, bson.D{{Key: "account_id", Value: accountID}})
}

func (r *AccountRepository) FindByIdentity(ctx context.Context, provider models.AuthProvider, subject string) (models.Account, error) {
	accounts, err := r.findAll(ctx)
	if err != nil {
		return models.Account{}, err
	}
	for _, account := range accounts {
		if account.Identities == nil || account.Identities.Data() == nil || account.Identities.Data().Rows == nil {
			continue
		}
		for _, identity := range account.Identities.Data().Rows.GetValueSlice() {
			if identity.Provider == provider && identity.Subject == subject {
				return account, nil
			}
		}
	}
	return models.Account{}, models.ErrAccountNotFound
}

func (r *AccountRepository) FindByRefreshTokenHash(ctx context.Context, tokenHash string, now time.Time) (models.Account, error) {
	accounts, err := r.findAll(ctx)
	if err != nil {
		return models.Account{}, err
	}
	for _, account := range accounts {
		if token := findRefreshToken(account, tokenHash, "", now); token != nil {
			return account, nil
		}
	}
	return models.Account{}, models.ErrAccountNotFound
}

func (r *AccountRepository) LinkPlayer(ctx context.Context, accountID, playerID string, now time.Time) error {
	if accountID == "" || playerID == "" {
		return models.ErrInvalidAccount
	}

	result, err := r.updateOne(ctx, bson.D{{Key: "account_id", Value: accountID}}, bson.D{{Key: "$addToSet", Value: bson.D{{Key: "player_ids", Value: playerID}}}, {Key: "$set", Value: bson.D{{Key: "updated_at", Value: now}}}})
	if err != nil {
		return fmt.Errorf("link player: %w", err)
	}
	_ = result

	return nil
}

func (r *AccountRepository) StoreRefreshToken(ctx context.Context, accountID string, token models.RefreshToken, now time.Time) error {
	if accountID == "" || token.TokenHash == "" || token.InstallID == "" || token.ExpiresAt.IsZero() {
		return models.ErrInvalidAccount
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = now
	}

	account, err := r.FindByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("store refresh token: %w", err)
	}
	key := nextRefreshTokenKey(account)
	update := bson.D{{Key: "$set", Value: bson.D{{Key: "refresh_tokens.rows." + key, Value: token}, {Key: "updated_at", Value: now}}}}
	result, err := r.updateOne(ctx, bson.D{{Key: "account_id", Value: accountID}}, update)
	if err != nil {
		return fmt.Errorf("store refresh token: %w", err)
	}
	_ = result

	return nil
}

func (r *AccountRepository) RevokeRefreshToken(ctx context.Context, accountID, tokenHash string, now time.Time) error {
	if accountID == "" || tokenHash == "" {
		return models.ErrInvalidAccount
	}

	account, err := r.FindByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	key := refreshTokenKey(account, tokenHash)
	if key == "" {
		return models.ErrAccountNotFound
	}
	update := bson.D{{Key: "$set", Value: bson.D{{Key: "refresh_tokens.rows." + key + ".revoked_at", Value: now}, {Key: "updated_at", Value: now}}}}
	result, err := r.updateOne(ctx, filterForAccount(accountID), update)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	_ = result

	return nil
}

func (r *AccountRepository) RotateRefreshToken(ctx context.Context, accountID, tokenHash, installID string, now time.Time, replacement models.RefreshToken) (models.Account, error) {
	account, err := r.FindByID(ctx, accountID)
	if err != nil {
		return models.Account{}, err
	}
	key := refreshTokenKey(account, tokenHash)
	if key == "" || findRefreshToken(account, tokenHash, installID, now) == nil {
		return models.Account{}, models.ErrAccountNotFound
	}
	newKey := nextRefreshTokenKey(account)
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "refresh_tokens.rows." + key + ".revoked_at", Value: now},
		{Key: "refresh_tokens.rows." + newKey, Value: replacement},
	}}}
	var updated models.Account
	if err := r.driver.Apply(ctx, filterForAccount(accountID), update, &updated); err != nil {
		if errors.Is(err, qmgo.ErrNoSuchDocuments) {
			return models.Account{}, models.ErrAccountNotFound
		}
		return models.Account{}, fmt.Errorf("rotate refresh token: %w", err)
	}
	return updated, nil
}

func (r *AccountRepository) findOne(ctx context.Context, filter bson.D) (models.Account, error) {
	if r.driver == nil {
		return models.Account{}, errors.New("mongo account repository collection is required")
	}

	var account models.Account
	err := r.driver.Client.Find(ctx, filter).One(&account)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return models.Account{}, models.ErrAccountNotFound
	}
	if err != nil {
		return models.Account{}, fmt.Errorf("find account: %w", err)
	}

	return account, nil
}

func (r *AccountRepository) updateOne(ctx context.Context, filter, update bson.D) (models.Account, error) {
	if r.driver == nil {
		return models.Account{}, errors.New("mongo account repository driver is required")
	}

	var account models.Account
	err := r.driver.Client.Find(ctx, filter).Apply(qmgo.Change{Update: update, ReturnNew: true}, &account)
	if errors.Is(err, qmgo.ErrNoSuchDocuments) {
		return models.Account{}, models.ErrAccountNotFound
	}
	if err != nil {
		return models.Account{}, err
	}

	return account, nil
}

func (r *AccountRepository) findAll(ctx context.Context) ([]models.Account, error) {
	if r.driver == nil {
		return nil, errors.New("mongo account repository driver is required")
	}
	accounts := make([]models.Account, 0)
	if err := r.driver.Client.Find(ctx, bson.D{}).All(&accounts); err != nil {
		return nil, fmt.Errorf("find accounts: %w", err)
	}
	return accounts, nil
}

func filterForAccount(accountID string) bson.D {
	return bson.D{{Key: "account_id", Value: accountID}}
}

func refreshTokenKey(account models.Account, tokenHash string) string {
	if account.RefreshTokens == nil || account.RefreshTokens.Data() == nil || account.RefreshTokens.Data().Rows == nil {
		return ""
	}
	var key string
	account.RefreshTokens.Data().Rows.Foreach2(func(id int64, node *md.Node[*models.RefreshToken]) bool {
		if node.Data() != nil && node.Data().TokenHash == tokenHash {
			key = fmt.Sprint(id)
			return true
		}
		return false
	})
	return key
}

func findRefreshToken(account models.Account, tokenHash, installID string, now time.Time) *models.RefreshToken {
	if account.RefreshTokens == nil || account.RefreshTokens.Data() == nil || account.RefreshTokens.Data().Rows == nil {
		return nil
	}
	var found *models.RefreshToken
	account.RefreshTokens.Data().Rows.Foreach2(func(_ int64, node *md.Node[*models.RefreshToken]) bool {
		token := node.Data()
		if token == nil || token.TokenHash != tokenHash || (installID != "" && token.InstallID != installID) || token.RevokedAt != nil || !now.Before(token.ExpiresAt) {
			return false
		}
		found = token
		return true
	})
	return found
}

func nextRefreshTokenKey(account models.Account) string {
	key := time.Now().UnixNano()
	for account.RefreshTokens != nil && account.RefreshTokens.Data() != nil && account.RefreshTokens.Data().Rows != nil {
		if _, exists := account.RefreshTokens.Data().Rows.Get2(key); !exists {
			return fmt.Sprint(key)
		}
		key++
	}
	return fmt.Sprint(key)
}
