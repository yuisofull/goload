package main

import "github.com/kelseyhightower/envconfig"

// Config holds all environment variables for the API gateway service.
//
// Required environment variables:
//
// LOG_LEVEL                             (default: debug)
// HTTP_ADDRESS                          (default: 0.0.0.0:8080)
// TOKEN_HMAC_SECRET                     (default: dev-secret-change-me)
// REDIS_ADDRESS                         (default: localhost:6379)
// REDIS_USERNAME
// REDIS_PASSWORD
// AUTH_SERVICE_GRPC_ADDRESS             (default: localhost:8081)
// TASK_SERVICE_GRPC_ADDRESS             (default: localhost:8082)
// MINIO_ENDPOINT
// MINIO_ACCESS_KEY
// MINIO_SECRET_KEY
// MINIO_BUCKET                          (default: goload)
// MINIO_USE_SSL                         (default: false)
// CORS_ALLOWED_ORIGINS                  (default: *)
// CORS_ALLOWED_METHODS                  (default: GET,POST,PUT,PATCH,DELETE,OPTIONS)
// CORS_ALLOWED_HEADERS                  (default: Authorization,Content-Type,Accept,Origin)
// CORS_EXPOSED_HEADERS                  (default: Content-Length,Content-Range,Content-Disposition)
// CORS_ALLOW_CREDENTIALS                (default: false)
// CORS_PREFLIGHT_MAX_AGE                (default: 600)
type Config struct {
	LogLevel               string `envconfig:"LOG_LEVEL"                 default:"debug"`
	HTTPAddress            string `envconfig:"HTTP_ADDRESS"              default:"0.0.0.0:8080"`
	TokenHMACSecret        string `envconfig:"TOKEN_HMAC_SECRET"         default:"dev-secret-change-me"`
	RedisAddress           string `envconfig:"REDIS_ADDRESS"             default:"localhost:6379"`
	RedisUsername          string `envconfig:"REDIS_USERNAME"`
	RedisPassword          string `envconfig:"REDIS_PASSWORD"`
	AuthServiceGRPCAddress string `envconfig:"AUTH_SERVICE_GRPC_ADDRESS" default:"localhost:8081"`
	TaskServiceGRPCAddress string `envconfig:"TASK_SERVICE_GRPC_ADDRESS" default:"localhost:8082"`
	MinioEndpoint          string `envconfig:"MINIO_ENDPOINT"`
	MinioAccessKey         string `envconfig:"MINIO_ACCESS_KEY"`
	MinioSecretKey         string `envconfig:"MINIO_SECRET_KEY"`
	MinioBucket            string `envconfig:"MINIO_BUCKET"              default:"goload"`
	MinioUseSSL            bool   `envconfig:"MINIO_USE_SSL"             default:"false"`
	CORSAllowedOrigins     string `envconfig:"CORS_ALLOWED_ORIGINS"      default:"*"`
	CORSAllowedMethods     string `envconfig:"CORS_ALLOWED_METHODS"      default:"GET,POST,PUT,PATCH,DELETE,OPTIONS"`
	CORSAllowedHeaders     string `envconfig:"CORS_ALLOWED_HEADERS"      default:"Authorization,Content-Type,Accept,Origin"`
	CORSExposedHeaders     string `envconfig:"CORS_EXPOSED_HEADERS"      default:"Content-Length,Content-Range,Content-Disposition"`
	CORSAllowCredentials   bool   `envconfig:"CORS_ALLOW_CREDENTIALS"    default:"false"`
	CORSPreflightMaxAge    int    `envconfig:"CORS_PREFLIGHT_MAX_AGE"    default:"600"`
}

func loadConfig() (*Config, error) {
	cfg := &Config{}
	return cfg, envconfig.Process("", cfg)
}
