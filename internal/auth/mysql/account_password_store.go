package mysql

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
		OfAccountID:    accountPassword.OfAccountID,
		HashedPassword: accountPassword.HashedPassword,
	})
}

func (a *accountPasswordStore) UpdateAccountPassword(ctx context.Context, accountPassword *auth.AccountPassword) error {
	q := a.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	return q.UpdateAccountPassword(ctx, sqlc.UpdateAccountPasswordParams{
		OfAccountID:    accountPassword.OfAccountID,
		HashedPassword: accountPassword.HashedPassword,
	})
}
