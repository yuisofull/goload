package main

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all environment variables for the download service.
//
// Required environment variables:
//
// LOG_LEVEL                (default: debug)
// KAFKA_BROKERS            (comma-separated, required)
// KAFKA_VERSION            (default: 4.0.0)
// KAFKA_CONSUMER_GROUP     (default: download-service-group)
// MINIO_ENDPOINT           (required)
// MINIO_ACCESS_KEY         (required)
// MINIO_SECRET_KEY         (required)
// MINIO_BUCKET             (default: goload)
// MINIO_USE_SSL            (default: false)
// MINIO_FILE_EXPIRY         (optional, e.g. "720h" for 30 days; 0 means no expiry)
type Config struct {
	LogLevel           string        `envconfig:"LOG_LEVEL"            default:"debug"`
	KafkaBrokers       []string      `envconfig:"KAFKA_BROKERS"`
	KafkaVersion       string        `envconfig:"KAFKA_VERSION"        default:"4.0.0"`
	KafkaConsumerGroup string        `envconfig:"KAFKA_CONSUMER_GROUP" default:"download-service-group"`
	MinioEndpoint      string        `envconfig:"MINIO_ENDPOINT"`
	MinioAccessKey     string        `envconfig:"MINIO_ACCESS_KEY"`
	MinioSecretKey     string        `envconfig:"MINIO_SECRET_KEY"`
	MinioBucket        string        `envconfig:"MINIO_BUCKET"         default:"goload"`
	MinioUseSSL        bool          `envconfig:"MINIO_USE_SSL"        default:"false"`
	MinioFileExpiry    time.Duration `envconfig:"MINIO_FILE_EXPIRY"    default:"0"`
}

func loadConfig() (*Config, error) {
	cfg := &Config{}
	return cfg, envconfig.Process("", cfg)
}
