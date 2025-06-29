package config

type AuthHashConfig struct {
	Bcrypt BcryptConfig `yaml:"bcrypt"`
}

type BcryptConfig struct {
	HashCost int `yaml:"hash_cost"`
}

type AuthTokenConfig struct {
	ExpiresIn                   string `yaml:"expires_in"`
	RegenerateTokenBeforeExpiry string `yaml:"regenerate_token_before_expiry"`
}

type AuthConfig struct {
	Hash  AuthHashConfig  `yaml:"hash"`
	Token AuthTokenConfig `yaml:"token"`
}
