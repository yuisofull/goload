package auth

type AccountPassword struct {
	OfAccountID    uint64 `sql:"of_account_id"`
	HashedPassword string `sql:"hashed_password"`
}
