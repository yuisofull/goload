package auth

type AccountPassword struct {
	OfAccountID uint64 `sql:"of_account_id"`
	Hash        string `sql:"hash"`
}
