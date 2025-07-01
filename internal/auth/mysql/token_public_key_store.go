package authmysql

import (
	"context"
	"database/sql"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/auth/mysql/sqlc"
)

type tokenPublicKeyStore struct {
	queries *sqlc.Queries
}

func NewTokenPublicKeyStore(db *sql.DB) auth.TokenPublicKeyStore {
	return &tokenPublicKeyStore{
		queries: sqlc.New(db),
	}
}

func (t *tokenPublicKeyStore) CreateTokenPublicKey(ctx context.Context, tokenPublicKey *auth.TokenPublicKey) (kid uint64, err error) {
	q := t.queries
	if tx, ok := getTxFrom(ctx); ok {
		q = q.WithTx(tx)
	}
	result, err := q.CreateTokenPublicKey(ctx, tokenPublicKey.PublicKey)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(id), nil
}

func (t *tokenPublicKeyStore) GetTokenPublicKey(ctx context.Context, kid uint64) (auth.TokenPublicKey, error) {
	q := t.queries
	tokenPublicKey, err := q.GetTokenPublicKey(ctx, kid)
	if err != nil {
		return auth.TokenPublicKey{}, err
	}
	return auth.TokenPublicKey{
		Id:        tokenPublicKey.ID,
		PublicKey: tokenPublicKey.PublicKey,
	}, nil
}
