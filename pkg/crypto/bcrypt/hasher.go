package bcrypt

import "golang.org/x/crypto/bcrypt"

type Hasher struct {
	hashCost int
}

func (h *Hasher) Hash(password []byte) (hashed []byte, err error) {
	return bcrypt.GenerateFromPassword(password, h.hashCost)
}

func (h *Hasher) Compare(hash, plaintext []byte) (isMatch bool, err error) {
	err = bcrypt.CompareHashAndPassword(hash, plaintext)
	if err != nil {
		return false, err
	}

	return true, nil
}

func NewHasher(hashCost int) *Hasher {
	return &Hasher{
		hashCost: hashCost,
	}
}
