package auth

import (
	"context"
	"errors"
	"github.com/yuisofull/goload/pkg/crypto"
)

var (
	ErrInvalidPassword = errors.New("invalid password")
)

type passwordHasher struct {
	hasher crypto.Hasher
}

func NewPasswordHasher(hasher crypto.Hasher) PasswordHasher {
	return &passwordHasher{
		hasher: hasher,
	}
}

func (h *passwordHasher) Hash(ctx context.Context, password string) (string, error) {
	hashed, err := h.hasher.Hash([]byte(password))
	if err != nil {
		return "", err
	}

	return string(hashed), nil
}

func (h *passwordHasher) Verify(ctx context.Context, password, hash string) error {
	isMatch, err := h.hasher.Compare([]byte(hash), []byte(password))
	if err != nil {
		return err
	}

	if !isMatch {
		return ErrInvalidPassword
	}

	return nil
}
