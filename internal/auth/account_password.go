package auth

type AccountPassword struct {
	OfAccountId uint64 `sql:"of_account_id"`
	Hash        string `sql:"hash"`
}
