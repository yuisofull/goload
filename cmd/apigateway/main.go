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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/yuisofull/goload/internal/apigateway"
	"github.com/yuisofull/goload/internal/auth"
	authtransport "github.com/yuisofull/goload/internal/auth/transport"
	"github.com/yuisofull/goload/internal/configs"
	"github.com/yuisofull/goload/internal/task"
	downloadtasktransport "github.com/yuisofull/goload/internal/task/transport"
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
		conn, err := grpc.NewClient(config.AuthService.GRPC.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			level.Error(logger).Log("err", err, "msg", "failed to connect to auth service")
			os.Exit(1)
		}
		defer conn.Close()

		authService = authtransport.NewGRPCClient(conn, logger)
	}

	// Connect to Download Task Service via gRPC
	var downloadTaskService task.Service
	{
		conn, err := grpc.NewClient(config.DownloadTaskService.GRPC.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			level.Error(logger).Log("err", err, "msg", "failed to connect to download task service")
			os.Exit(1)
		}
		defer conn.Close()

		downloadTaskService = downloadtasktransport.NewGRPCClient(conn, logger)
	}

	// Create token validator for authentication middleware
	var tokenValidator auth.SessionValidator = authService

	// Create authentication middleware
	authMiddleware := apigateway.NewAuthMiddleware(tokenValidator)

	// Create unified gateway endpoints with authentication middleware
	gatewayEndpoints := apigateway.NewGatewayEndpoints(downloadTaskService, authMiddleware)

	// Create HTTP handler
	httpHandler := apigateway.NewHTTPHandler(gatewayEndpoints, logger)

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
			select {
			case <-ctx.Done():
				return ctx.Err()
			}
		}, func(error) {
		})
	}

	level.Info(logger).Log("exit", g.Run())
}
