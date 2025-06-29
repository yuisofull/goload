package crypto

type Hasher interface {
	Hash(password []byte) (hashed []byte, err error)
	Compare(hash, plaintext []byte) (isMatch bool, err error)
}
