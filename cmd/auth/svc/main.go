package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	kitgrpc "github.com/go-kit/kit/transport/grpc"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/redis/go-redis/v9"
	"github.com/yuisofull/goload/internal/auth"
	authcache "github.com/yuisofull/goload/internal/auth/cache"
	authendpoint "github.com/yuisofull/goload/internal/auth/endpoint"
	authmysql "github.com/yuisofull/goload/internal/auth/mysql"
	authpb "github.com/yuisofull/goload/internal/auth/pb"
	authtransport "github.com/yuisofull/goload/internal/auth/transport"
	"github.com/yuisofull/goload/internal/configs"
	rediscache "github.com/yuisofull/goload/pkg/cache/redis"
	"github.com/yuisofull/goload/pkg/crypto/bcrypt"
	"google.golang.org/grpc"
	"net"
	"os"
	"os/signal"
	"syscall"
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

	var redisClient *redis.Client
	{
		redisClient = redis.NewClient(&redis.Options{
			Addr:     config.Redis.Address,
			Username: config.Redis.Username,
			Password: config.Redis.Password,
		})

		_, err = redisClient.Ping(ctx).Result()
		if err != nil {
			level.Error(logger).Log("err", err)
			os.Exit(1)
		}
	}

	var store authmysql.Store
	{
		var closeStore func()
		store, closeStore, err = authmysql.New(config.MySQL)
		if err != nil {
			level.Error(logger).Log("err", err)
			os.Exit(1)
		}
		defer closeStore()
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, config.Auth.Token.JWTRS512.RSABits)
	if err != nil {
		level.Error(logger).Log("err", err)
		os.Exit(1)
	}

	var (
		tokenManager   auth.TokenManager
		tokenStore     = store.TokenPublicKeyStore
		publicKeyCache = rediscache.New[authcache.TokenPublicKeyCacheKey, []byte](redisClient, "auth:token_public_key")
	)

	{
		tokenStore = authcache.NewTokenPublicKeyStore(publicKeyCache, store)
		tokenManager, err = auth.NewJWTRS512TokenManager(privateKey, config.Auth.Token.ExpiresIn, tokenStore)
		if err != nil {
			level.Error(logger).Log("err", err)
			os.Exit(1)
		}
	}

	var (
		bcryptHasher = bcrypt.NewHasher(config.Auth.Hash.Bcrypt.HashCost)
		hasher       = auth.NewPasswordHasher(bcryptHasher)
		nameCache    = rediscache.New[authcache.AccountNameTakenSetKey, string](redisClient, "auth:account_name")
		accountStore = authcache.NewAccountStore(nameCache, store)
		service      = auth.NewService(accountStore, store, store, hasher, tokenManager)
		endpointSet  = authendpoint.New(service)
		grpcServer   = authtransport.NewGRPCServer(endpointSet, logger)
	)

	var g run.Group
	{
		grpcListener, err := net.Listen("tcp", config.AuthService.GRPC.Address)
		if err != nil {
			level.Error(logger).Log("transport", "gRPC", "during", "Listen", "err", err)
			os.Exit(1)
		}

		baseServer := grpc.NewServer(grpc.UnaryInterceptor(kitgrpc.Interceptor))
		authpb.RegisterAuthServiceServer(baseServer, grpcServer)

		g.Add(func() error {
			level.Info(logger).Log("transport", "gRPC", "addr", config.AuthService.GRPC.Address)
			return baseServer.Serve(grpcListener)
		}, func(error) {
			baseServer.GracefulStop()
			_ = grpcListener.Close()
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
