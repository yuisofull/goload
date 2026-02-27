package configs

import "time"

type Kafka struct {
	Brokers             []string      `yaml:"brokers"`
	Version             string        `yaml:"version"`
	ConsumerGroup       string        `yaml:"consumer_group"`
	ClientID            string        `yaml:"client_id"`
	MaxRetry            int           `yaml:"max_retry"`
	AutoCommit          bool          `yaml:"auto_commit"`
	NackResendSleep     time.Duration `yaml:"nack_resend_sleep"`
	ReconnectRetrySleep time.Duration `yaml:"reconnect_retry_sleep"`
}

type Messaging struct {
	Kafka Kafka `yaml:"kafka"`
}
