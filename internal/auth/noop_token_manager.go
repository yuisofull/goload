package auth

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yuisofull/goload/internal/errors"
)

type noopTokenManager struct {
	expiresIn time.Duration
}

// NewNoopTokenManager returns a simple, non-cryptographic TokenManager
// intended for pocket/single-user mode. Tokens are plain strings in the
// format: "pocket:<accountID>:<expiryUnix>".
func NewNoopTokenManager(expiresIn time.Duration) TokenManager {
	return &noopTokenManager{expiresIn: expiresIn}
}

func (n *noopTokenManager) Sign(accountID uint64) (string, error) {
	expiry := time.Now().Add(n.expiresIn).Unix()
	return fmt.Sprintf("pocket:%d:%d", accountID, expiry), nil
}

func (n *noopTokenManager) GetAccountIDFrom(token string) (uint64, error) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 || parts[0] != "pocket" {
		return 0, &errors.Error{Code: ErrCodeInvalidToken, Message: "invalid token format"}
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, &errors.Error{Code: ErrCodeInvalidToken, Message: "invalid account id"}
	}
	return id, nil
}

func (n *noopTokenManager) GetExpiryFrom(token string) (time.Time, error) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 || parts[0] != "pocket" {
		return time.Time{}, &errors.Error{Code: ErrCodeInvalidToken, Message: "invalid token format"}
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return time.Time{}, &errors.Error{Code: ErrCodeInvalidToken, Message: "invalid expiry"}
	}
	return time.Unix(exp, 0), nil
}
