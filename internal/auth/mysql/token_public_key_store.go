package authmysql

import (
	"context"
	"database/sql"
	stderrors "errors"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/auth/mysql/sqlc"
	"github.com/yuisofull/goload/internal/errors"
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
	result, err := q.CreateTokenPublicKey(ctx, string(tokenPublicKey.PublicKey))
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), nil
}

func (t *tokenPublicKeyStore) GetTokenPublicKey(ctx context.Context, kid uint64) (auth.TokenPublicKey, error) {
	q := t.queries
	tokenPublicKey, err := q.GetTokenPublicKey(ctx, kid)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return auth.TokenPublicKey{}, errors.ErrNotFound
		}
		return auth.TokenPublicKey{}, err
	}
	return auth.TokenPublicKey{
		Id:        tokenPublicKey.ID,
		PublicKey: []byte(tokenPublicKey.PublicKey),
	}, nil
}
