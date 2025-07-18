package mysql

import (
	"context"
	"database/sql"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/auth/mysql/sqlc"
)

type txManager struct {
	queries *sqlc.Queries
	db      *sql.DB
}

func NewTxManager(db *sql.DB) auth.TxManager {
	return &txManager{
		queries: sqlc.New(db),
		db:      db,
	}
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

func getTxFrom(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	if !ok {
		return nil, false
	}
	return tx, true
}
