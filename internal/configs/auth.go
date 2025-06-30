package configs

import "time"

type AuthHashConfig struct {
	Bcrypt Bcrypt `yaml:"bcrypt"`
}

type Bcrypt struct {
	HashCost int `yaml:"hash_cost"`
}

type Token struct {
	ExpiresIn                   time.Duration `yaml:"expires_in"`
	RegenerateTokenBeforeExpiry time.Duration `yaml:"regenerate_token_before_expiry"`
}

type Auth struct {
	Hash  AuthHashConfig `yaml:"hash"`
	Token Token          `yaml:"token"`
}
