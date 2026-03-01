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
	"github.com/yuisofull/goload/internal/configs"
	"github.com/yuisofull/goload/internal/storage"
	taskpkg "github.com/yuisofull/goload/internal/task"
	tasktransport "github.com/yuisofull/goload/internal/task/transport"
	rediscache "github.com/yuisofull/goload/pkg/cache/redis"
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

	// Connect to Auth Service via gRPC
	var authService auth.Service
	{
		conn, err := grpc.NewClient(
			config.AuthService.GRPC.Address,
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
			config.DownloadTaskService.GRPC.Address,
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
	gatewayEndpoints := apigateway.NewGatewayEndpoints(downloadTaskService, authMiddleware, authService)

	// Create redis client and token store
	var tokenStore taskpkg.TokenStore
	{
		redisClient := redis.NewClient(
			&redis.Options{
				Addr:     config.Redis.Address,
				Username: config.Redis.Username,
				Password: config.Redis.Password,
			},
		)
		secret := []byte(config.APIGateway.TokenHMACSecret)
		if len(secret) == 0 {
			secret = []byte("default-secret-change-me")
		}
		// create a redis-backed cache and pass it into the generic token store constructor
		rc := rediscache.New[string, storage.TokenMetadata](redisClient)
		tokenStore = taskpkg.NewTokenStore(rc, secret)
	}

	// Create storage backend (MinIO) if config provided; otherwise nil
	var storageBackend storage.Reader
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
			}
		}
	}

	// Create HTTP handler (with download handler that uses token store and storage backend)
	httpHandler := apigateway.NewHTTPHandlerWithDownload(gatewayEndpoints, logger, storageBackend, tokenStore)

	var g run.Group
	{
		// HTTP server
		httpServer := &http.Server{
			Addr:    config.APIGateway.HTTP.Address,
			Handler: httpHandler,
		}

		g.Add(func() error {
			level.Info(logger).Log("transport", "HTTP", "addr", config.APIGateway.HTTP.Address)
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
