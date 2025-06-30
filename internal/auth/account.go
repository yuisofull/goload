package auth

type Account struct {
	ID          uint64 `sql:"id"`
	AccountName string `sql:"account_name"`
}
