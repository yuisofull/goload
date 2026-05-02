package main

import "github.com/kelseyhightower/envconfig"

type Config struct {
	LogLevel             string `envconfig:"LOG_LEVEL"              default:"debug"`
	HTTPAddress          string `envconfig:"HTTP_ADDRESS"           default:"0.0.0.0:8080"`
	PocketDBPath         string `envconfig:"POCKET_DB_PATH"         default:"./goload.db"`
	PocketDataDir        string `envconfig:"POCKET_DATA_DIR"        default:"./data"`
	PocketWebDir         string `envconfig:"POCKET_WEB_DIR"`
	TokenHMACSecret      string `envconfig:"TOKEN_HMAC_SECRET"      default:"dev-secret-change-me"`
	AuthTokenRSABits     int    `envconfig:"AUTH_TOKEN_RSA_BITS"    default:"2048"`
	AuthTokenExpiresIn   string `envconfig:"AUTH_TOKEN_EXPIRES_IN"  default:"24h"`
	AuthHashBcryptCost   int    `envconfig:"AUTH_HASH_BCRYPT_COST"  default:"10"`
	CORSAllowedOrigins   string `envconfig:"CORS_ALLOWED_ORIGINS"   default:"*"`
	CORSAllowedMethods   string `envconfig:"CORS_ALLOWED_METHODS"   default:"GET,POST,PUT,PATCH,DELETE,OPTIONS"`
	CORSAllowedHeaders   string `envconfig:"CORS_ALLOWED_HEADERS"   default:"Authorization,Content-Type,Accept,Origin"`
	CORSExposedHeaders   string `envconfig:"CORS_EXPOSED_HEADERS"   default:"Content-Length,Content-Range,Content-Disposition"`
	CORSAllowCredentials bool   `envconfig:"CORS_ALLOW_CREDENTIALS" default:"false"`
	CORSPreflightMaxAge  int    `envconfig:"CORS_PREFLIGHT_MAX_AGE" default:"600"`
}

func loadConfig() (*Config, error) {
	cfg := &Config{}
	return cfg, envconfig.Process("", cfg)
}
