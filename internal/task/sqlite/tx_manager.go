package sqlite

import (
	"context"
	"github.com/go-llsqlite/crawshaw/sqlitex"
	task "github.com/yuisofull/goload/internal/task"
)

type connKey struct{}

type txManager struct {
	pool *sqlitex.Pool
}

func NewTxManager(pool *sqlitex.Pool) task.TxManager {
	return &txManager{pool: pool}
}

func (m *txManager) DoInTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	conn := m.pool.Get(ctx)
	if conn == nil {
		return context.DeadlineExceeded
	}
	defer m.pool.Put(conn)

	defer sqlitex.Save(conn)(&err)
	ctx = context.WithValue(ctx, connKey{}, conn)
	return fn(ctx)
}
