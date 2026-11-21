package rabbitmq

import (
	"cmp"
	"errors"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	DefaultExchange       = "goload.dispatch"
	DefaultQueue          = "task.dispatch"
	DefaultDeliveryLimit  = 5
	DefaultPrefetch       = 4
	DefaultConfirmTimeout = 30 * time.Second
	DefaultReconnectDelay = time.Second

	deadLetterExchangeSuffix = ".dlx"
	deadLetterQueueSuffix    = ".dlq"
)

var (
	ErrURLEmpty   = errors.New("rabbitmq: URL is empty")
	ErrURLInvalid = errors.New("rabbitmq: URL is not a valid AMQP URI")
)

type Config struct {
	URL string

	Exchange           string
	Queue              string
	DeadLetterExchange string
	DeadLetterQueue    string

	DeliveryLimit int

	Prefetch       int
	ConfirmTimeout time.Duration
	ReconnectDelay time.Duration
}

func resolveConfig(cfg *Config) (Config, error) {
	if cfg == nil {
		return Config{}, ErrURLEmpty
	}

	resolved := cfg.withDefaults()
	if err := resolved.validate(); err != nil {
		return Config{}, err
	}
	return resolved, nil
}

func (c Config) withDefaults() Config {
	c.Exchange = cmp.Or(c.Exchange, DefaultExchange)
	c.Queue = cmp.Or(c.Queue, DefaultQueue)
	c.DeadLetterExchange = cmp.Or(c.DeadLetterExchange, c.Exchange+deadLetterExchangeSuffix)
	c.DeadLetterQueue = cmp.Or(c.DeadLetterQueue, c.Queue+deadLetterQueueSuffix)
	c.DeliveryLimit = positiveOr(c.DeliveryLimit, DefaultDeliveryLimit)
	c.Prefetch = positiveOr(c.Prefetch, DefaultPrefetch)
	c.ConfirmTimeout = positiveOr(c.ConfirmTimeout, DefaultConfirmTimeout)
	c.ReconnectDelay = positiveOr(c.ReconnectDelay, DefaultReconnectDelay)
	return c
}

func (c Config) validate() error {
	if c.URL == "" {
		return ErrURLEmpty
	}
	// The parse error echoes the URI, password included, so it is dropped.
	if _, err := amqp.ParseURI(c.URL); err != nil {
		return ErrURLInvalid
	}
	return nil
}

func positiveOr[T cmp.Ordered](value, fallback T) T {
	var zero T
	if value > zero {
		return value
	}
	return fallback
}
