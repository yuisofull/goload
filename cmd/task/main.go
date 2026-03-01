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

	kitgrpc "github.com/go-kit/kit/transport/grpc"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"

	"github.com/yuisofull/goload/internal/configs"

	"github.com/IBM/sarama"
	"google.golang.org/grpc"

	taskpkg "github.com/yuisofull/goload/internal/task"
	taskendpoint "github.com/yuisofull/goload/internal/task/endpoint"
	taskmysql "github.com/yuisofull/goload/internal/task/mysql"
	taskpb "github.com/yuisofull/goload/internal/task/pb"
	tasktransport "github.com/yuisofull/goload/internal/task/transport"
	"github.com/yuisofull/goload/pkg/message"
	kafkapkg "github.com/yuisofull/goload/pkg/message/kafka"
)

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
		logger = level.NewFilter(logger, level.Allow(level.ParseDefault(config.Log.Level, level.DebugValue())))
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}

	// Setup MySQL
	var db *sql.DB
	{
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			config.MySQL.Username,
			config.MySQL.Password,
			config.MySQL.Host,
			config.MySQL.Port,
			config.MySQL.Database)
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			level.Error(logger).Log("msg", "failed to open mysql", "err", err)
			os.Exit(1)
		}
		// simple ping retry
		for i := 0; i < 5; i++ {
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
	if len(config.Messaging.Kafka.Brokers) > 0 {
		kv, err := sarama.ParseKafkaVersion(config.Messaging.Kafka.Version)
		if err != nil {
			level.Error(logger).Log("msg", "failed to parse kafka version, falling back", "err", err)
			kv = sarama.V2_0_0_0
		}
		pubCfg := &kafkapkg.PublisherConfig{
			BrokerHosts: config.Messaging.Kafka.Brokers,
			Version:     kv,
			MaxRetry:    config.Messaging.Kafka.MaxRetry,
		}
		if pub, err = kafkapkg.NewPublisher(pubCfg, kafkapkg.WithLogger(logger)); err != nil {
			level.Error(logger).Log("msg", "failed to create kafka publisher", "err", err)
			os.Exit(1)
		}
	}

	// event publisher wrapper
	dep := taskpkg.NewEventPublisher(pub)

	// NewService expects a value Publisher (not pointer) in this package
	svc := taskpkg.NewService(repo, *dep, tx)

	endpointSet := taskendpoint.New(svc)

	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(kitgrpc.Interceptor))

	// register service
	pbServer := tasktransport.NewGRPCServer(endpointSet, logger)
	// register pb server using generated protobuf package
	taskpb.RegisterTaskServiceServer(grpcServer, pbServer)

	var g run.Group

	{
		lis, err := net.Listen("tcp", config.DownloadTaskService.GRPC.Address)
		if err != nil {
			level.Error(logger).Log("msg", "failed to listen", "err", err)
			os.Exit(1)
		}

		g.Add(func() error {
			level.Info(logger).Log("transport", "gRPC", "addr", config.DownloadTaskService.GRPC.Address)
			return grpcServer.Serve(lis)
		}, func(error) {
			grpcServer.GracefulStop()
			_ = lis.Close()
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
