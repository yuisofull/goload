package auth

type Account struct {
	AccountID   uint64 `sql:"account_id"`
	AccountName string `sql:"account_name"`
}
