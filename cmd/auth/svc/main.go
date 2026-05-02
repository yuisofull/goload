package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
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
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	"github.com/yuisofull/goload/internal/auth"
	authcache "github.com/yuisofull/goload/internal/auth/cache"
	authendpoint "github.com/yuisofull/goload/internal/auth/endpoint"
	authmysql "github.com/yuisofull/goload/internal/auth/mysql"
	authpb "github.com/yuisofull/goload/internal/auth/pb"
	authtransport "github.com/yuisofull/goload/internal/auth/transport"
	rediscache "github.com/yuisofull/goload/pkg/cache/redis"
	"github.com/yuisofull/goload/pkg/crypto/bcrypt"
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

	var redisClient *redis.Client
	{
		redisClient = redis.NewClient(&redis.Options{
			Addr:     config.RedisAddress,
			Username: config.RedisUsername,
			Password: config.RedisPassword,
		})

		_, err = redisClient.Ping(ctx).Result()
		if err != nil {
			level.Error(logger).Log("err", err)
			os.Exit(1)
		}
	}

	var mysqlDB *sql.DB
	{
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			config.MySQLUsername,
			config.MySQLPassword,
			config.MySQLHost,
			config.MySQLPort,
			config.MySQLDatabase)
		mysqlDB, err = sql.Open("mysql", dsn)
		if err != nil {
			level.Error(logger).Log("err", err)
			os.Exit(1)
		}
		// ping to verify connection
		for range 5 {
			err = mysqlDB.Ping()
			if err == nil {
				break
			}
			level.Error(logger).Log("err", err, "msg", "failed to connect to mysql, retrying...")
			// wait before retrying
			<-time.After(2 * time.Second)
		}
	}

	var store *authmysql.Store
	{
		store = authmysql.New(mysqlDB)
		defer store.Close()
	}

	var (
		tokenManager   auth.TokenManager
		tokenStore     = store.TokenPublicKeyStore
		publicKeyCache = rediscache.New(
			redisClient,
			rediscache.WithKeyEncoder[authcache.TokenPublicKeyCacheKey, []byte](
				rediscache.PrefixKeyEncoder[authcache.TokenPublicKeyCacheKey]{
					Prefix: "auth:token_public_key",
					Inner:  rediscache.DefaultKeyEncoder[authcache.TokenPublicKeyCacheKey]{},
				},
			),
		)
	)
	{
		cacheErrorHandler := func(ctx context.Context, err error) {
			level.Error(logger).Log("err", err)
		}
		tokenStore = authcache.NewTokenPublicKeyStore(publicKeyCache, store, cacheErrorHandler)
		privateKey, err := rsa.GenerateKey(rand.Reader, config.AuthTokenRSABits)
		if err != nil {
			level.Error(logger).Log("err", err)
			os.Exit(1)
		}
		tokenExpiresIn, err := time.ParseDuration(config.AuthTokenExpiresIn)
		if err != nil {
			level.Error(logger).Log("err", err, "msg", "invalid AUTH_TOKEN_EXPIRES_IN")
			os.Exit(1)
		}
		tokenManager, err = auth.NewJWTRS512TokenManager(privateKey, tokenExpiresIn, tokenStore)
		if err != nil {
			level.Error(logger).Log("err", err)
			os.Exit(1)
		}
	}

	var (
		bcryptHasher = bcrypt.NewHasher(config.AuthHashBcryptCost)
		hasher       = auth.NewPasswordHasher(bcryptHasher)
		nameCache    = rediscache.New(
			redisClient,
			rediscache.WithKeyEncoder[authcache.AccountNameTakenSetKey, string](
				rediscache.PrefixKeyEncoder[authcache.AccountNameTakenSetKey]{
					Prefix: "auth:account_name",
					Inner:  rediscache.DefaultKeyEncoder[authcache.AccountNameTakenSetKey]{},
				},
			),
		)
		cacheErrorHandler = func(ctx context.Context, err error) {
			level.Error(logger).Log("err", err)
		}
		accountStore = authcache.NewAccountStore(nameCache, store, cacheErrorHandler)
		service      = auth.NewService(accountStore, store, store, hasher, tokenManager)
		endpointSet  = authendpoint.New(service)
		grpcServer   = authtransport.NewGRPCServer(endpointSet, logger)
	)

	var g run.Group
	{
		grpcListener, err := net.Listen("tcp", config.GRPCAddress)
		if err != nil {
			level.Error(logger).Log("transport", "gRPC", "during", "Listen", "err", err)
			os.Exit(1)
		}

		baseServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
			middleware.LoggingGRPCInterceptor(logger),
			kitgrpc.Interceptor,
		))
		authpb.RegisterAuthServiceServer(baseServer, grpcServer)

		g.Add(func() error {
			for svcName, svcInfo := range baseServer.GetServiceInfo() {
				for _, m := range svcInfo.Methods {
					level.Info(logger).Log("msg", "API endpoint registered", "service", svcName, "method", m.Name)
				}
			}
			level.Info(logger).Log(
				"transport", "gRPC",
				"addr", config.GRPCAddress,
				"msg", "serving grpc endpoints",
			)
			return baseServer.Serve(grpcListener)
		}, func(error) {
			baseServer.GracefulStop()
			_ = grpcListener.Close()
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
