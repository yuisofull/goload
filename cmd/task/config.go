package main

import "github.com/kelseyhightower/envconfig"

// Config holds all environment variables for the task service.
//
// Required environment variables:
//
// LOG_LEVEL                                      (default: debug)
// MYSQL_HOST                                     (default: localhost)
// MYSQL_PORT                                     (default: 3306)
// MYSQL_USERNAME                                 (default: root)
// MYSQL_PASSWORD
// MYSQL_DATABASE                                 (default: goload)
// REDIS_ADDRESS                                  (default: localhost:6379)
// REDIS_USERNAME
// REDIS_PASSWORD
// KAFKA_BROKERS                                  (comma-separated list)
// KAFKA_VERSION                                  (default: 4.0.0)
// KAFKA_MAX_RETRY                                (default: 3)
// GRPC_ADDRESS                                   (default: 0.0.0.0:8082)
// TOKEN_HMAC_SECRET                              (default: dev-secret-change-me)
// MINIO_ENDPOINT
// MINIO_ACCESS_KEY
// MINIO_SECRET_KEY
// MINIO_BUCKET                                   (default: goload)
// MINIO_TASK_SOURCES_BUCKET                       (default: task-sources)
// MINIO_USE_SSL                                  (default: false)
// MINIO_PRESIGN_PUBLIC_ENDPOINT
// MINIO_PRESIGN_ACCESS_KEY
// MINIO_PRESIGN_SECRET_KEY
type Config struct {
	LogLevel                   string   `envconfig:"LOG_LEVEL"                     default:"debug"`
	MySQLHost                  string   `envconfig:"MYSQL_HOST"                    default:"localhost"`
	MySQLPort                  int      `envconfig:"MYSQL_PORT"                    default:"3306"`
	MySQLUsername              string   `envconfig:"MYSQL_USERNAME"                default:"root"`
	MySQLPassword              string   `envconfig:"MYSQL_PASSWORD"`
	MySQLDatabase              string   `envconfig:"MYSQL_DATABASE"                default:"goload"`
	RedisAddress               string   `envconfig:"REDIS_ADDRESS"                 default:"localhost:6379"`
	RedisUsername              string   `envconfig:"REDIS_USERNAME"`
	RedisPassword              string   `envconfig:"REDIS_PASSWORD"`
	KafkaBrokers               []string `envconfig:"KAFKA_BROKERS"`
	KafkaVersion               string   `envconfig:"KAFKA_VERSION"                 default:"4.0.0"`
	KafkaMaxRetry              int      `envconfig:"KAFKA_MAX_RETRY"               default:"3"`
	GRPCAddress                string   `envconfig:"GRPC_ADDRESS"                  default:"0.0.0.0:8082"`
	TokenHMACSecret            string   `envconfig:"TOKEN_HMAC_SECRET"             default:"dev-secret-change-me"`
	MinioEndpoint              string   `envconfig:"MINIO_ENDPOINT"`
	MinioAccessKey             string   `envconfig:"MINIO_ACCESS_KEY"`
	MinioSecretKey             string   `envconfig:"MINIO_SECRET_KEY"`
	MinioBucket                string   `envconfig:"MINIO_BUCKET"                  default:"goload"`
	MinioTaskSourcesBucket     string   `envconfig:"MINIO_TASK_SOURCES_BUCKET"     default:"task-sources"`
	MinioUseSSL                bool     `envconfig:"MINIO_USE_SSL"                 default:"false"`
	MinioPresignPublicEndpoint string   `envconfig:"MINIO_PRESIGN_PUBLIC_ENDPOINT"`
	MinioPresignAccessKey      string   `envconfig:"MINIO_PRESIGN_ACCESS_KEY"`
	MinioPresignSecretKey      string   `envconfig:"MINIO_PRESIGN_SECRET_KEY"`
}

func loadConfig() (*Config, error) {
	cfg := &Config{}
	return cfg, envconfig.Process("", cfg)
}
