package auth

type Account struct {
	Id          uint64 `sql:"id"`
	AccountName string `sql:"account_name"`
}
