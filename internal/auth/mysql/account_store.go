package authmysql

import (
	"context"
	"database/sql"
	stderrors "errors"
	_ "github.com/go-sql-driver/mysql"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/auth/mysql/sqlc"
	"github.com/yuisofull/goload/internal/errors"
)

type accountStore struct {
	queries *sqlc.Queries
}

func NewAccountStore(db *sql.DB) auth.AccountStore {
	return &accountStore{
		queries: sqlc.New(db),
	}
}

func (a *accountStore) CreateAccount(ctx context.Context, account *auth.Account) (uint64, error) {
	q := a.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	result, err := q.CreateAccount(ctx, account.AccountName)
	if err != nil {
		return 0, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "failed to create account",
			Cause:   err,
		}
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "failed to get last insert id",
			Cause:   err,
		}
	}
	return uint64(id), nil
}

func (a *accountStore) GetAccountByID(ctx context.Context, id uint64) (*auth.Account, error) {
	q := a.queries
	account, err := q.GetAccountByID(ctx, id)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, errors.ErrNotFound
		}
		return nil, err
	}
	return &auth.Account{
		Id:          account.ID,
		AccountName: account.AccountName,
	}, nil
}

func (a *accountStore) GetAccountByAccountName(ctx context.Context, accountName string) (*auth.Account, error) {
	q := a.queries
	account, err := q.GetAccountByAccountName(ctx, accountName)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return nil, errors.ErrNotFound
		}
		return nil, err
	}
	return &auth.Account{
		Id:          account.ID,
		AccountName: account.AccountName,
	}, nil
}
