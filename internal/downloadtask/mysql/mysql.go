package downloadtaskmysql

import (
	"database/sql"
	"github.com/yuisofull/goload/internal/downloadtask"
)

type Store struct {
	downloadtask.Store
	downloadtask.TxManager
	*sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{
		Store:     NewStore(db),
		TxManager: NewTxManager(db),
		DB:        db,
	}
}
