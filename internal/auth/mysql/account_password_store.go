package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/auth/mysql/sqlc"
	"github.com/yuisofull/goload/internal/config"
)

type accountPasswordStore struct {
	queries *sqlc.Queries
}

func NewAccountPasswordStore(config config.MySQLConfig) (auth.AccountPasswordStore, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", config.Username, config.Password, config.Host, config.Port, config.Database)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	return &accountPasswordStore{
		queries: sqlc.New(db),
	}, nil
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
