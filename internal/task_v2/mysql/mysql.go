package downloadtaskmysql

import (
	"database/sql"
	"github.com/yuisofull/goload/internal/task_v2"
)

type Store struct {
	task_v2.Store
	task_v2.TxManager
	*sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{
		Store:     NewStore(db),
		TxManager: NewTxManager(db),
		DB:        db,
	}
}
