package downloadtaskmysql

import (
	"database/sql"
	"github.com/yuisofull/goload/internal/task"
)

type Store struct {
	task.Store
	task.TxManager
	*sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{
		Store:     NewStore(db),
		TxManager: NewTxManager(db),
		DB:        db,
	}
}
