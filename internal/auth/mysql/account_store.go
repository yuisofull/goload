package mysql

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/auth/mysql/sqlc"
	"github.com/yuisofull/goload/internal/config"
)

type accountStore struct {
	queries *sqlc.Queries
}

func NewAccountStore(config config.MySQLConfig) (auth.AccountStore, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", config.Username, config.Password, config.Host, config.Port, config.Database)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	return &accountStore{
		queries: sqlc.New(db),
	}, nil
}

func getTxFrom(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	if !ok {
		return nil, false
	}
	return tx, true
}

func (a *accountStore) CreateAccount(ctx context.Context, account *auth.Account) (uint64, error) {
	q := a.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	result, err := q.CreateAccount(ctx, account.AccountName)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(id), nil
}

func (a *accountStore) GetAccountByID(ctx context.Context, id uint64) (*auth.Account, error) {
	q := a.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	account, err := q.GetAccountByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &auth.Account{
		AccountID:   account.AccountID,
		AccountName: account.AccountName,
	}, nil
}

func (a *accountStore) GetAccountByAccountName(ctx context.Context, accountName string) (*auth.Account, error) {
	q := a.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	account, err := q.GetAccountByAccountName(ctx, accountName)
	if err != nil {
		return nil, err
	}
	return &auth.Account{
		AccountID:   account.AccountID,
		AccountName: account.AccountName,
	}, nil
}
