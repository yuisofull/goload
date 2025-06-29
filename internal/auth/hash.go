package auth

import (
	"context"
	"errors"
	"github.com/yuisofull/goload/pkg/crypto"
)

var (
	ErrInvalidPassword = errors.New("invalid password")
)

type ph struct {
	hasher crypto.Hasher
}

func NewPasswordHasher(hasher crypto.Hasher) *ph {
	return &ph{
		hasher: hasher,
	}
}

func (h *ph) Hash(ctx context.Context, password string) (string, error) {
	hashed, err := h.hasher.Hash([]byte(password))
	if err != nil {
		return "", err
	}

	return string(hashed), nil
}

func (h *ph) Verify(ctx context.Context, password, hash string) error {
	isMatch, err := h.hasher.Compare([]byte(hash), []byte(password))
	if err != nil {
		return err
	}

	if !isMatch {
		return ErrInvalidPassword
	}

	return nil
}
