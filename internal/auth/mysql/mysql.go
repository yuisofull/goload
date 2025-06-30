package authmysql

import (
	"database/sql"
	"fmt"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/configs"
)

type Store struct {
	auth.AccountStore
	auth.AccountPasswordStore
	auth.TxManager
}

func New(config configs.MySQL) (Store, func(), error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", config.Username, config.Password, config.Host, config.Port, config.Database)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return Store{}, nil, err
	}
	return Store{
			AccountStore:         NewAccountStore(db),
			AccountPasswordStore: NewAccountPasswordStore(db),
			TxManager:            NewTxManager(db),
		}, func() {
			db.Close()
		}, nil
}
