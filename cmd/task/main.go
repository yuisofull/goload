package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	kitgrpc "github.com/go-kit/kit/transport/grpc"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	storagepkg "github.com/yuisofull/goload/internal/storage"
	taskpkg "github.com/yuisofull/goload/internal/task"
	taskendpoint "github.com/yuisofull/goload/internal/task/endpoint"
	taskmysql "github.com/yuisofull/goload/internal/task/mysql"
	taskpb "github.com/yuisofull/goload/internal/task/pb"
	tasktransport "github.com/yuisofull/goload/internal/task/transport"
	"github.com/yuisofull/goload/pkg/message"
	kafkapkg "github.com/yuisofull/goload/pkg/message/kafka"
	"github.com/yuisofull/goload/pkg/middleware"
)

func main() {
	config, err := loadConfig()
	if err != nil {
		panic(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
		logger = level.NewFilter(logger, level.Allow(level.ParseDefault(config.LogLevel, level.DebugValue())))
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}

	// Setup MySQL
	var db *sql.DB
	{
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			config.MySQLUsername,
			config.MySQLPassword,
			config.MySQLHost,
			config.MySQLPort,
			config.MySQLDatabase)
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			level.Error(logger).Log("msg", "failed to open mysql", "err", err)
			os.Exit(1)
		}
		// simple ping retry
		for range 5 {
			if err = db.Ping(); err == nil {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if err != nil {
			level.Error(logger).Log("msg", "cannot connect to mysql", "err", err)
			os.Exit(1)
		}
	}

	// repo and tx manager
	repo := taskmysql.NewTaskRepo(db)
	tx := taskmysql.NewTxManager(db)

	// messaging publisher (kafka) if configured
	var pub message.Publisher
	if len(config.KafkaBrokers) > 0 {
		kv, err := sarama.ParseKafkaVersion(config.KafkaVersion)
		if err != nil {
			level.Error(logger).Log("msg", "failed to parse kafka version, falling back", "err", err)
			kv = sarama.V3_6_0_0
		}
		pubCfg := &kafkapkg.PublisherConfig{
			BrokerHosts: config.KafkaBrokers,
			Version:     kv,
			MaxRetry:    config.KafkaMaxRetry,
		}
		if pub, err = kafkapkg.NewPublisher(pubCfg, kafkapkg.WithLogger(logger)); err != nil {
			level.Error(logger).Log("msg", "failed to create kafka publisher", "err", err)
			os.Exit(1)
		}
	}

	// event publisher wrapper
	dep := taskpkg.NewEventPublisher(pub)

	// Optional: presigner (MinIO) and token store (Redis) for GenerateDownloadURL
	var svcOpts []taskpkg.ServiceOption
	if config.MinioEndpoint != "" {
		taskSourcesBucket := config.MinioTaskSourcesBucket
		if taskSourcesBucket == "" {
			taskSourcesBucket = "task-sources"
		}

		// Storage for large task source payloads (e.g. uploaded .torrent files)
		sourceStore, err := storagepkg.NewMinioBackend(
			config.MinioEndpoint,
			config.MinioAccessKey,
			config.MinioSecretKey,
			config.MinioUseSSL,
			taskSourcesBucket,
			storagepkg.WithExpiry(24*time.Hour),
		)
		if err != nil {
			level.Error(logger).
				Log("msg", "failed to create minio backend for task sources", "bucket", taskSourcesBucket, "err", err)
			os.Exit(1)
		}
		svcOpts = append(svcOpts, taskpkg.WithTaskSourceStore(sourceStore))

		presignAccessKey := config.MinioAccessKey
		presignSecretKey := config.MinioSecretKey
		if config.MinioPresignAccessKey != "" {
			presignAccessKey = config.MinioPresignAccessKey
			presignSecretKey = config.MinioPresignSecretKey
		}
		presignEndpoint := config.MinioPresignPublicEndpoint
		if presignEndpoint == "" {
			presignEndpoint = config.MinioEndpoint
		}

		var presigner *storagepkg.Minio
		presigner, err = storagepkg.NewMinioPresigner(
			presignEndpoint,
			presignAccessKey,
			presignSecretKey,
			config.MinioUseSSL,
			config.MinioBucket,
		)
		if err != nil {
			level.Error(logger).Log("msg", "failed to create minio presigner", "err", err)
		}

		if presigner != nil {
			level.Info(logger).Log("msg", "minio presigner initialized",
				"public_url", presignEndpoint,
				"proxy_url", config.MinioEndpoint,
				"access_key", presignAccessKey)
			svcOpts = append(svcOpts, taskpkg.WithPresigner(nil))

			taskSourcePresigner, err := storagepkg.NewMinioPresigner(
				presignEndpoint,
				presignAccessKey,
				presignSecretKey,
				config.MinioUseSSL,
				taskSourcesBucket,
			)
			if err != nil {
				level.Error(logger).
					Log("msg", "failed to create minio presigner for task sources", "bucket", taskSourcesBucket, "err", err)
				os.Exit(1)
			}
			svcOpts = append(svcOpts, taskpkg.WithTaskSourcePresigner(taskSourcePresigner))
		} else {
			level.Warn(logger).Log("msg", "minio presigner NOT available — download-url will use token fallback")
		}
	} else {
		level.Warn(logger).Log("msg", "taskservice.storage.minio.endpoint not configured — presign disabled")
	}
	{
		redisClient := redis.NewClient(&redis.Options{
			Addr:     config.RedisAddress,
			Username: config.RedisUsername,
			Password: config.RedisPassword,
		})
		secret := []byte(config.TokenHMACSecret)
		if len(secret) == 0 {
			secret = []byte("default-secret-change-me")
		}
		ts := taskpkg.NewRedisTokenStore(redisClient, secret)
		svcOpts = append(svcOpts, taskpkg.WithTokenStore(ts))
	}

	svc := taskpkg.NewService(repo, *dep, tx, append(svcOpts, taskpkg.WithLogger(logger))...)

	endpointSet := taskendpoint.New(svc)

	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		middleware.LoggingGRPCInterceptor(logger),
		kitgrpc.Interceptor,
	))

	// register service
	pbServer := tasktransport.NewGRPCServer(endpointSet, logger)
	// register pb server using generated protobuf package
	taskpb.RegisterTaskServiceServer(grpcServer, pbServer)

	// Kafka subscriber for consuming download-service events (task.completed, task.failed, progress)
	var taskEventConsumer *tasktransport.EventConsumer
	if len(config.KafkaBrokers) > 0 {
		kv2, err := sarama.ParseKafkaVersion(config.KafkaVersion)
		if err != nil {
			kv2 = sarama.V3_6_0_0
		}
		subCfg := &kafkapkg.SubscriberConfig{
			Brokers:       config.KafkaBrokers,
			ConsumerGroup: "task-service-group",
			Version:       kv2,
		}
		taskSub, err := kafkapkg.NewSubscriber(subCfg, kafkapkg.WithLog(logger))
		if err != nil {
			level.Error(logger).Log("msg", "failed to create kafka subscriber for task service", "err", err)
		} else {
			taskEventConsumer = tasktransport.NewEventConsumer(svc, taskSub)
		}
	}

	var g run.Group
	{
		lis, err := net.Listen("tcp", config.GRPCAddress)
		if err != nil {
			level.Error(logger).Log("msg", "failed to listen", "err", err)
			os.Exit(1)
		}

		g.Add(func() error {
			for svcName, svcInfo := range grpcServer.GetServiceInfo() {
				for _, m := range svcInfo.Methods {
					level.Info(logger).Log("msg", "API endpoint registered", "service", svcName, "method", m.Name)
				}
			}
			level.Info(logger).Log(
				"transport", "gRPC",
				"addr", config.GRPCAddress,
				"msg", "serving grpc endpoints",
			)
			return grpcServer.Serve(lis)
		}, func(error) {
			grpcServer.GracefulStop()
			_ = lis.Close()
		})
	}

	// Start the event consumer if it was successfully created
	if taskEventConsumer != nil {
		g.Add(func() error {
			return taskEventConsumer.Start(ctx)
		}, func(error) {
		})
	}

	{
		g.Add(func() error {
			<-ctx.Done()
			return ctx.Err()
		}, func(error) {})
	}

	level.Info(logger).Log("exit", g.Run())
}
