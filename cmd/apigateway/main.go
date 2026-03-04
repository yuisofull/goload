package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/yuisofull/goload/internal/apigateway"
	"github.com/yuisofull/goload/internal/auth"
	authtransport "github.com/yuisofull/goload/internal/auth/transport"
	storagepkg "github.com/yuisofull/goload/internal/storage"
	taskpkg "github.com/yuisofull/goload/internal/task"
	tasktransport "github.com/yuisofull/goload/internal/task/transport"
	rediscache "github.com/yuisofull/goload/pkg/cache/redis"
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

	// Connect to Auth Service via gRPC
	var authService auth.Service
	{
		conn, err := grpc.NewClient(
			config.AuthServiceGRPCAddress,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			level.Error(logger).Log("err", err, "msg", "failed to connect to auth service")
			os.Exit(1)
		}
		defer conn.Close()

		authService = authtransport.NewGRPCClient(conn, logger)
	}

	// Connect to Download Task Service via gRPC
	var downloadTaskService taskpkg.Service
	{
		conn, err := grpc.NewClient(
			config.TaskServiceGRPCAddress,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			level.Error(logger).Log("err", err, "msg", "failed to connect to download task service")
			os.Exit(1)
		}
		defer conn.Close()

		downloadTaskService = tasktransport.NewGRPCClient(conn, logger)
	}

	// Create token validator for authentication middleware
	var tokenValidator auth.SessionValidator = authService

	// Create authentication middleware
	authMiddleware := apigateway.NewAuthMiddleware(tokenValidator)

	// Create unified gateway endpoints with authentication middleware
	gatewayEndpoints := apigateway.NewGatewayEndpoints(
		downloadTaskService,
		authMiddleware,
		authService,
	)

	// Redis token store — consumed by the /download fallback handler
	var tokenStore taskpkg.TokenStore
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
		rc := rediscache.New[string, storagepkg.TokenMetadata](redisClient)
		tokenStore = taskpkg.NewTokenStore(rc, secret)
	}

	// MinIO storage backend for the /download fallback streamer
	var storageBackend storagepkg.Reader
	{
		if config.MinioEndpoint != "" && config.MinioAccessKey != "" && config.MinioSecretKey != "" && config.MinioBucket != "" {
			if m, err := storagepkg.NewMinioBackend(
				config.MinioEndpoint,
				config.MinioAccessKey,
				config.MinioSecretKey,
				config.MinioUseSSL,
				config.MinioBucket,
			); err == nil {
				storageBackend = m
			} else {
				level.Error(logger).Log("msg", "failed to initialize minio backend", "err", err)
			}
		}
	}

	// /tasks/download-url  → returns {url, direct} (presigned or token URL)
	// /download?token=...  → fallback: validate token, stream bytes from MinIO
	httpHandler := apigateway.NewHTTPHandlerWithDownload(gatewayEndpoints, logger, storageBackend, tokenStore)

	var g run.Group
	{
		// HTTP server
		httpServer := &http.Server{
			Addr:    config.HTTPAddress,
			Handler: httpHandler,
		}

		g.Add(func() error {
			level.Info(logger).Log("transport", "HTTP", "addr", config.HTTPAddress)
			return httpServer.ListenAndServe()
		}, func(error) {
			httpServer.Shutdown(ctx)
		})
	}

	{
		g.Add(func() error {
			<-ctx.Done()
			return ctx.Err()
		}, func(error) {
		})
	}

	level.Info(logger).Log("exit", g.Run())
}
