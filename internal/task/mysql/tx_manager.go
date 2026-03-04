package mysql

import (
	"context"
	"database/sql"

	"github.com/yuisofull/goload/internal/task"
)

type txManager struct {
	db *sql.DB
}

func NewTxManager(db *sql.DB) task.TxManager {
	return &txManager{db: db}
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
