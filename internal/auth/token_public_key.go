package auth

type TokenPublicKey struct {
	Id        uint64 `sql:"id"`
	PublicKey []byte `sql:"public_key"`
}
