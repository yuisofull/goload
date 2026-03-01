package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/IBM/sarama"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"

	"github.com/yuisofull/goload/internal/configs"
	"github.com/yuisofull/goload/internal/download"
	downloadtransport "github.com/yuisofull/goload/internal/download/transport"
	"github.com/yuisofull/goload/internal/storage"
	"github.com/yuisofull/goload/pkg/message"
	kafkapkg "github.com/yuisofull/goload/pkg/message/kafka"
)

// printfAdapter adapts go-kit logger to the Printf interface expected by downloadtransport
type printfAdapter struct{ l log.Logger }

func (p printfAdapter) Printf(format string, v ...interface{}) {
	_ = level.Debug(p.l).Log("msg", fmt.Sprintf(format, v...))
}

func main() {
	config, err := configs.Load()
	if err != nil {
		panic(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
		logger = level.NewFilter(logger, level.Allow(level.DebugValue()))
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}

	// use printfAdapter to adapt logger

	// storage backend (MinIO) optional
	var storageBackend storage.Backend
	{
		minioCfg := config.APIGateway.Storage.Minio
		if minioCfg.Endpoint != "" && minioCfg.AccessKey != "" && minioCfg.SecretKey != "" && minioCfg.Bucket != "" {
			if m, err := storage.NewMinioBackend(
				minioCfg.Endpoint,
				minioCfg.AccessKey,
				minioCfg.SecretKey,
				minioCfg.UseSSL,
				minioCfg.Bucket,
			); err == nil {
				storageBackend = m
			} else {
				level.Error(logger).Log("msg", "failed to initialize minio backend", "err", err)
				os.Exit(1)
			}
		} else {
			level.Error(logger).Log("msg", "no storage backend configured for download service")
			os.Exit(1)
		}
	}

	// create publisher/subscriber: try Kafka if configured, otherwise use in-memory
	var pub message.Publisher
	var sub message.Subscriber
	if len(config.Messaging.Kafka.Brokers) > 0 {
		// parse kafka version from config
		kv, err := sarama.ParseKafkaVersion(config.Messaging.Kafka.Version)
		if err != nil {
			level.Error(logger).
				Log("msg", "failed to parse kafka version from config, falling back to default", "err", err)
			kv = sarama.V2_0_0_0
		}
		// create kafka publisher
		pubCfg := &kafkapkg.PublisherConfig{BrokerHosts: config.Messaging.Kafka.Brokers, Version: kv}
		pub, err = kafkapkg.NewPublisher(pubCfg)
		if err != nil {
			level.Error(logger).Log("msg", "failed to create kafka publisher", "err", err)
			os.Exit(1)
		}
		subCfg := &kafkapkg.SubscriberConfig{
			Brokers:       config.Messaging.Kafka.Brokers,
			ConsumerGroup: config.Messaging.Kafka.ConsumerGroup,
			Version:       kv,
		}
		sub, err = kafkapkg.NewSubscriber(subCfg, kafkapkg.WithErrorHandler(func(_ context.Context, e error) {
			// Ignore benign context cancellation errors (happen during shutdown) and log them at debug level.
			if errors.Is(e, context.Canceled) || e.Error() == "context canceled" {
				_ = level.Debug(logger).Log("msg", "kafka subscriber canceled", "err", e)
				return
			}
			level.Error(logger).Log("msg", "kafka subscriber error", "err", e)
		}), kafkapkg.WithLog(logger))
		if err != nil {
			level.Error(logger).Log("msg", "failed to create kafka subscriber", "err", err)
			os.Exit(1)
		}
	} else {
		level.Error(logger).
			Log("msg", "no messaging backend configured: please configure Kafka brokers for the download service")
		os.Exit(1)
	}

	dep := download.NewDownloadEventPublisher(pub)
	svc := download.NewService(storageBackend, dep)

	consumer := downloadtransport.NewEventConsumer(svc, sub, printfAdapter{logger})

	var g run.Group
	// run the consumer
	{
		g.Add(func() error {
			return consumer.Start(ctx)
		}, func(error) {
			// on interrupt, cancel context which will stop consumer
			cancel()
		})
	}

	// wait for signal; on shutdown attempt to close pub/sub cleanly
	{
		g.Add(func() error {
			<-ctx.Done()
			return ctx.Err()
		}, func(error) {
			// attempt graceful close of publisher and subscriber
			if pub != nil {
				if err := pub.Close(); err != nil {
					level.Error(logger).Log("msg", "error closing publisher", "err", err)
				}
			}
			if sub != nil {
				if err := sub.Close(); err != nil {
					level.Error(logger).Log("msg", "error closing subscriber", "err", err)
				}
			}
		})
	}

	level.Info(logger).Log("exit", g.Run())
}
