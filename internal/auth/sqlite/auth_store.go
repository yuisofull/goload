package sqlite

import (
	"context"
	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"
	auth "github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/errors"
)

type authStore struct {
	pool *sqlitex.Pool
}

type AuthStore struct {
	AccountStore         auth.AccountStore
	AccountPasswordStore auth.AccountPasswordStore
	TxManager            auth.TxManager
}

func New(pool *sqlitex.Pool) *AuthStore {
	store := &authStore{pool: pool}
	return &AuthStore{
		AccountStore:         store,
		AccountPasswordStore: store,
		TxManager:            NewTxManager(pool),
	}
}

func (s *authStore) getConn(ctx context.Context) *sqlite.Conn {
	if conn, ok := ctx.Value(connKey{}).(*sqlite.Conn); ok {
		return conn
	}
	return nil
}

func (s *authStore) withConn(ctx context.Context, fn func(conn *sqlite.Conn) error) error {
	conn := s.getConn(ctx)
	if conn != nil {
		return fn(conn)
	}
	conn = s.pool.Get(ctx)
	if conn == nil {
		return context.DeadlineExceeded
	}
	defer s.pool.Put(conn)
	return fn(conn)
}

func (s *authStore) CreateAccount(ctx context.Context, a *auth.Account) (uint64, error) {
	var id int64
	err := s.withConn(ctx, func(conn *sqlite.Conn) error {
		err := sqlitex.Execute(conn, `INSERT INTO accounts (account_name) VALUES (?)`, &sqlitex.ExecOptions{
			Args: []any{a.AccountName},
		})
		if err != nil {
			return err
		}
		id = conn.LastInsertRowID()
		return nil
	})
	return uint64(id), err
}

func (s *authStore) GetAccountByID(ctx context.Context, id uint64) (*auth.Account, error) {
	var a *auth.Account
	err := s.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn, `SELECT id, account_name FROM accounts WHERE id = ?`, &sqlitex.ExecOptions{
			Args: []any{id},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				a = &auth.Account{
					Id:          uint64(stmt.ColumnInt64(0)),
					AccountName: stmt.ColumnText(1),
				}
				return nil
			},
		})
	})
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errors.ErrNotFound
	}
	return a, nil
}

func (s *authStore) GetAccountByAccountName(ctx context.Context, accountName string) (*auth.Account, error) {
	var a *auth.Account
	err := s.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn, `SELECT id, account_name FROM accounts WHERE account_name = ?`, &sqlitex.ExecOptions{
			Args: []any{accountName},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				a = &auth.Account{
					Id:          uint64(stmt.ColumnInt64(0)),
					AccountName: stmt.ColumnText(1),
				}
				return nil
			},
		})
	})
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errors.ErrNotFound
	}
	return a, nil
}

func (s *authStore) CreateAccountPassword(ctx context.Context, ap *auth.AccountPassword) error {
	return s.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn, `INSERT INTO account_passwords (of_account_id, hashed_password) VALUES (?, ?)`, &sqlitex.ExecOptions{
			Args: []any{ap.OfAccountId, ap.HashedPassword},
		})
	})
}

func (s *authStore) UpdateAccountPassword(ctx context.Context, ap *auth.AccountPassword) error {
	return s.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn, `UPDATE account_passwords SET hashed_password = ? WHERE of_account_id = ?`, &sqlitex.ExecOptions{
			Args: []any{ap.HashedPassword, ap.OfAccountId},
		})
	})
}

func (s *authStore) GetAccountPassword(ctx context.Context, ofAccountID uint64) (auth.AccountPassword, error) {
	var ap auth.AccountPassword
	var found bool
	err := s.withConn(ctx, func(conn *sqlite.Conn) error {
		return sqlitex.Execute(conn, `SELECT of_account_id, hashed_password FROM account_passwords WHERE of_account_id = ?`, &sqlitex.ExecOptions{
			Args: []any{ofAccountID},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				ap = auth.AccountPassword{
					OfAccountId:    uint64(stmt.ColumnInt64(0)),
					HashedPassword: stmt.ColumnText(1),
				}
				found = true
				return nil
			},
		})
	})
	if err != nil {
		return auth.AccountPassword{}, err
	}
	if !found {
		return auth.AccountPassword{}, errors.ErrNotFound
	}
	return ap, nil
}
