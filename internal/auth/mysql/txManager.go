package mysql

import (
	"context"
	"database/sql"
	"github.com/yuisofull/goload/internal/auth/mysql/sqlc"
)

type txManager struct {
	queries *sqlc.Queries
	db      *sql.DB
}

type txKey struct{}

func (t *txManager) DoInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := t.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ctx = context.WithValue(ctx, txKey{}, tx)

	if err := fn(ctx); err != nil {
		return err
	}

	return tx.Commit()
}
