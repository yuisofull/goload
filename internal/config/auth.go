package config

type AuthHashConfig struct {
	Cost int `yaml:"cost"`
}

type AuthTokenConfig struct {
	ExpiresIn                   string `yaml:"expires_in"`
	RegenerateTokenBeforeExpiry string `yaml:"regenerate_token_before_expiry"`
}

type AuthConfig struct {
	Hash  AuthHashConfig  `yaml:"hash"`
	Token AuthTokenConfig `yaml:"token"`
}
