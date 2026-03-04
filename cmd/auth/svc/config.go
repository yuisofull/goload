package main

import "github.com/kelseyhightower/envconfig"

// Config holds all environment variables for the auth service.
//
// Required environment variables:
//
// LOG_LEVEL                            (default: debug)
// MYSQL_HOST                           (default: localhost)
// MYSQL_PORT                           (default: 3306)
// MYSQL_USERNAME                       (default: root)
// MYSQL_PASSWORD
// MYSQL_DATABASE                       (default: goload)
// REDIS_ADDRESS                        (default: localhost:6379)
// REDIS_USERNAME
// REDIS_PASSWORD
// AUTH_HASH_BCRYPT_COST                (default: 10)
// AUTH_TOKEN_RSA_BITS                  (default: 2048)
// AUTH_TOKEN_EXPIRES_IN                (default: 24h)
// AUTH_TOKEN_REGENERATE_BEFORE_EXPIRY  (default: 1h)
// AUTH_SERVICE_GRPC_ADDRESS            (default: 0.0.0.0:8081)
type Config struct {
	LogLevel                        string `envconfig:"LOG_LEVEL"                           default:"debug"`
	MySQLHost                       string `envconfig:"MYSQL_HOST"                          default:"localhost"`
	MySQLPort                       int    `envconfig:"MYSQL_PORT"                          default:"3306"`
	MySQLUsername                   string `envconfig:"MYSQL_USERNAME"                      default:"root"`
	MySQLPassword                   string `envconfig:"MYSQL_PASSWORD"`
	MySQLDatabase                   string `envconfig:"MYSQL_DATABASE"                      default:"goload"`
	RedisAddress                    string `envconfig:"REDIS_ADDRESS"                       default:"localhost:6379"`
	RedisUsername                   string `envconfig:"REDIS_USERNAME"`
	RedisPassword                   string `envconfig:"REDIS_PASSWORD"`
	AuthHashBcryptCost              int    `envconfig:"AUTH_HASH_BCRYPT_COST"               default:"10"`
	AuthTokenRSABits                int    `envconfig:"AUTH_TOKEN_RSA_BITS"                 default:"2048"`
	AuthTokenExpiresIn              string `envconfig:"AUTH_TOKEN_EXPIRES_IN"               default:"24h"`
	AuthTokenRegenerateBeforeExpiry string `envconfig:"AUTH_TOKEN_REGENERATE_BEFORE_EXPIRY" default:"1h"`
	GRPCAddress                     string `envconfig:"AUTH_SERVICE_GRPC_ADDRESS"           default:"0.0.0.0:8081"`
}

func loadConfig() (*Config, error) {
	cfg := &Config{}
	return cfg, envconfig.Process("", cfg)
}
