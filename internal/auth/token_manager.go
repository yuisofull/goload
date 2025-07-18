package auth

import (
	"context"
	"crypto/rsa"
	errstderrors "errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/yuisofull/goload/internal/errors"
	pkgrsa "github.com/yuisofull/goload/pkg/crypto/rsa"
	"time"
)

type TokenPublicKeyStore interface {
	CreateTokenPublicKey(ctx context.Context, tokenPublicKey *TokenPublicKey) (kid uint64, err error)
	GetTokenPublicKey(ctx context.Context, kid uint64) (TokenPublicKey, error)
}

type TokenManager interface {
	Sign(accountID uint64) (string, error)
	GetAccountIDFrom(token string) (uint64, error)
	GetExpiryFrom(token string) (time.Time, error)
}

type jwtRS256TokenManager struct {
	privateKey *rsa.PrivateKey
	kid        uint64
	expiresIn  time.Duration
	store      TokenPublicKeyStore
}

func NewJWTRS512TokenManager(
	privateKey *rsa.PrivateKey,
	expiresIn time.Duration,
	store TokenPublicKeyStore,
) (TokenManager, error) {
	publicKey := &privateKey.PublicKey
	pemBytes, err := pkgrsa.SerializePublicKey(publicKey)
	if err != nil {
		return nil, err
	}

	kid, err := store.CreateTokenPublicKey(context.Background(), &TokenPublicKey{
		PublicKey: pemBytes,
	})
	if err != nil {
		return nil, err
	}

	return &jwtRS256TokenManager{
		kid:        kid,
		privateKey: privateKey,
		expiresIn:  expiresIn,
		store:      store,
	}, nil
}

func (t *jwtRS256TokenManager) Sign(accountID uint64) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS512, jwt.MapClaims{
		"sub":        accountID,
		"account_id": accountID,
		"exp":        time.Now().Add(t.expiresIn).Unix(),
		"iat":        time.Now().Unix(),
		"jti":        time.Now().Unix(),
		"kid":        t.kid,
		"iss":        "authservice",
	})

	tokenStr, err := token.SignedString(t.privateKey)
	if err != nil {
		return "", &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "failed to sign token",
			Cause:   err,
		}
	}
	return tokenStr, nil
}

func (t *jwtRS256TokenManager) parseToken(tokenStr string) (*jwt.Token, error) {
	return jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok || token.Method.Alg() != jwt.SigningMethodRS512.Alg() {
			return nil, errstderrors.New("unexpected signing method")
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return nil, errstderrors.New("cannot get token's claims")
		}
		kid, ok := claims["kid"].(float64)
		if !ok {
			return nil, errstderrors.New("cannot get token's kid")
		}
		tokenPublicKey, err := t.store.GetTokenPublicKey(context.Background(), uint64(kid))
		if err != nil {
			return nil, err
		}
		publicKey, err := pkgrsa.DeserializePublicKey(tokenPublicKey.PublicKey)
		if err != nil {
			return nil, err
		}
		return publicKey, nil
	})
}

func (t *jwtRS256TokenManager) GetAccountIDFrom(tokenStr string) (uint64, error) {
	parsedToken, err := t.parseToken(tokenStr)

	if err != nil {
		return 0, &errors.Error{
			Code:    ErrCodeInvalidToken,
			Message: "cannot parse token",
			Cause:   err,
		}
	}

	if !parsedToken.Valid {
		return 0, &errors.Error{
			Code:    ErrCodeInvalidToken,
			Message: "invalid token",
			Cause:   err,
		}
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok {
		return 0, &errors.Error{
			Code:    ErrCodeInvalidToken,
			Message: "cannot get token's claims",
			Cause:   err,
		}
	}

	accountID, ok := claims["account_id"].(float64)
	if !ok {
		return 0, &errors.Error{
			Code:    ErrCodeInvalidToken,
			Message: "cannot get token's account id",
			Cause:   err,
		}
	}

	return uint64(accountID), nil

}

func (t *jwtRS256TokenManager) GetExpiryFrom(token string) (time.Time, error) {
	parsedToken, err := t.parseToken(token)
	if err != nil {
		return time.Time{}, err
	}

	if !parsedToken.Valid {
		return time.Time{}, &errors.Error{
			Code:    ErrCodeInvalidToken,
			Message: "invalid token",
			Cause:   err,
		}
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok {
		return time.Time{}, &errors.Error{
			Code:    ErrCodeInvalidToken,
			Message: "cannot get token's claims",
			Cause:   err,
		}
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return time.Time{}, &errors.Error{
			Code:    ErrCodeInvalidToken,
			Message: "cannot get token's expiry",
			Cause:   err,
		}
	}

	return time.Unix(int64(exp), 0), nil
}
