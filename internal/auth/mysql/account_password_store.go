package authmysql

import (
	"context"
	"database/sql"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/auth/mysql/sqlc"
)

type accountPasswordStore struct {
	queries *sqlc.Queries
}

func NewAccountPasswordStore(db *sql.DB) auth.AccountPasswordStore {
	return &accountPasswordStore{
		queries: sqlc.New(db),
	}
}

func (a *accountPasswordStore) CreateAccountPassword(ctx context.Context, accountPassword *auth.AccountPassword) error {
	q := a.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	return q.CreateAccountPassword(ctx, sqlc.CreateAccountPasswordParams{
		OfAccountID:    accountPassword.OfAccountId,
		HashedPassword: accountPassword.HashedPassword,
	})
}

func (a *accountPasswordStore) UpdateAccountPassword(ctx context.Context, accountPassword *auth.AccountPassword) error {
	q := a.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	return q.UpdateAccountPassword(ctx, sqlc.UpdateAccountPasswordParams{
		OfAccountID:    accountPassword.OfAccountId,
		HashedPassword: accountPassword.HashedPassword,
	})
}

func (a *accountPasswordStore) GetAccountPassword(ctx context.Context, ofAccountID uint64) (auth.AccountPassword, error) {
	q := a.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	accountPassword, err := q.GetAccountPassword(ctx, ofAccountID)
	if err != nil {
		return auth.AccountPassword{}, err
	}
	return auth.AccountPassword{
		OfAccountId:    accountPassword.OfAccountID,
		HashedPassword: accountPassword.HashedPassword,
	}, nil
}
