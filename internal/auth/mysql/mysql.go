package authmysql

import (
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
	"github.com/yuisofull/goload/internal/auth"
)

type Store struct {
	auth.AccountStore
	auth.AccountPasswordStore
	auth.TxManager
	auth.TokenPublicKeyStore
	*sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{
		AccountStore:         NewAccountStore(db),
		AccountPasswordStore: NewAccountPasswordStore(db),
		TxManager:            NewTxManager(db),
		TokenPublicKeyStore:  NewTokenPublicKeyStore(db),
		DB:                   db,
	}
}
