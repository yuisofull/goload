package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/go-llsqlite/crawshaw/sqlitex"
	"github.com/google/uuid"
	"github.com/oklog/run"

	"github.com/yuisofull/goload/internal/apigateway"
	"github.com/yuisofull/goload/internal/auth"
	authcache "github.com/yuisofull/goload/internal/auth/cache"
	authsqlite "github.com/yuisofull/goload/internal/auth/sqlite"
	"github.com/yuisofull/goload/internal/download"
	"github.com/yuisofull/goload/internal/download/downloader"
	downloadtransport "github.com/yuisofull/goload/internal/download/transport"
	"github.com/yuisofull/goload/internal/observability"
	"github.com/yuisofull/goload/internal/storage"
	"github.com/yuisofull/goload/internal/task"
	tasksqlite "github.com/yuisofull/goload/internal/task/sqlite"
	tasktransport "github.com/yuisofull/goload/internal/task/transport"
	inmemcache "github.com/yuisofull/goload/pkg/cache/inmem"
	"github.com/yuisofull/goload/pkg/crypto/bcrypt"
	"github.com/yuisofull/goload/pkg/message/inmem"
	"github.com/yuisofull/goload/pkg/middleware"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// inmemTokenPublicKeyStore is a thread-safe in-memory implementation of auth.TokenPublicKeyStore.
// RSA public keys are ephemeral and regenerated on each restart, so SQLite persistence is unnecessary.
type inmemTokenPublicKeyStore struct {
	mu   sync.RWMutex
	keys map[uint64]auth.TokenPublicKey
	seq  uint64
}

func newInmemTokenPublicKeyStore() *inmemTokenPublicKeyStore {
	return &inmemTokenPublicKeyStore{
		keys: make(map[uint64]auth.TokenPublicKey),
		seq:  1,
	}
}

func (s *inmemTokenPublicKeyStore) CreateTokenPublicKey(_ context.Context, k *auth.TokenPublicKey) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.seq
	s.seq++
	k.Id = id
	s.keys[id] = *k
	return id, nil
}

func (s *inmemTokenPublicKeyStore) GetTokenPublicKey(_ context.Context, kid uint64) (auth.TokenPublicKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.keys[kid]
	if !ok {
		return auth.TokenPublicKey{}, fmt.Errorf("token public key not found: %d", kid)
	}
	return k, nil
}

func main() {
	cfg, err := loadConfig()
	must(err)

	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
		logger = level.NewFilter(logger, level.Allow(level.ParseDefault(cfg.LogLevel, level.DebugValue())))
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}

	rec, metricsHandler, err := observability.NewRecorder("pocket")
	if err != nil {
		level.Error(logger).Log("msg", "failed to init metrics", "err", err)
		os.Exit(1)
	}
	defer func() { _ = rec.Shutdown(context.Background()) }()

	tracer, err := observability.NewTracer(context.Background(), "pocket", observability.TraceConfig{
		Endpoint:    cfg.TracingOTLPEndpoint,
		SampleRatio: cfg.TracingSampleRatio,
	})
	if err != nil {
		level.Error(logger).Log("msg", "failed to init tracing", "err", err)
		os.Exit(1)
	}
	defer func() { _ = tracer.Shutdown(context.Background()) }()
	observability.LogErrors(func(err error) { level.Warn(logger).Log("msg", "telemetry error", "err", err) })

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := sqlitex.Open(cfg.PocketDBPath, 0, 10)
	must(err)
	defer pool.Close()

	must(runMigrations(pool))

	storageBackend, err := storage.NewLocalBackend(
		cfg.PocketDataDir,
		storage.WithLocalLogger(logger),
		storage.WithLocalExpiry(24*time.Hour),
	)
	must(err)
	defer func() { _ = storageBackend.Close() }()

	b := inmem.NewBroker(100, logger)
	pub := inmem.NewPublisher(b)
	sub := inmem.NewSubscriber(b)

	authStore := authsqlite.New(pool)
	taskRepo := tasksqlite.NewTaskRepo(pool)
	tx := tasksqlite.NewTxManager(pool)
	outboxRepo := tasksqlite.NewOutboxRepo(pool)
	attemptRepo := tasksqlite.NewAttemptRepo(pool)
	reconcileRepo := tasksqlite.NewReconcileRepo(pool)

	var tokenManager auth.TokenManager
	{
		privateKey, err := rsa.GenerateKey(rand.Reader, cfg.AuthTokenRSABits)
		must(err)

		tokenExpiresIn, err := time.ParseDuration(cfg.AuthTokenExpiresIn)
		must(err)

		cacheErrorHandler := func(_ context.Context, err error) {
			level.Error(logger).Log("msg", "token public key cache error", "err", err)
		}
		cachedPKStore := authcache.NewTokenPublicKeyStore(
			inmemcache.New[authcache.TokenPublicKeyCacheKey, []byte](5*time.Minute),
			newInmemTokenPublicKeyStore(),
			cacheErrorHandler,
		)

		tokenManager, err = auth.NewJWTRS512TokenManager(privateKey, tokenExpiresIn, cachedPKStore)
		must(err)
	}

	bcryptHasher := bcrypt.NewHasher(cfg.AuthHashBcryptCost)
	hasher := auth.NewPasswordHasher(bcryptHasher)

	authSvc := auth.NewService(
		authStore.AccountStore,
		authStore.AccountPasswordStore,
		authStore.TxManager,
		hasher,
		tokenManager,
	)

	authMiddleware := apigateway.NewAuthMiddleware(authSvc)

	// The in-memory bus drops a message with no subscriber, so outbox delivery
	// must wait for the download consumer to subscribe: keep the row unsent
	// until it does.
	taskPub := task.NewEventPublisher(pub, task.WithOutboxTransport(task.NewRequireSubscriberTransport(pub)))
	// Outbox relay: publishes the TaskCreated rows that CreateTask/RetryTask
	// write in the task transaction, on the same in-memory bus as before.
	outboxRelay := task.NewOutboxRelay(outboxRepo, taskPub, task.WithOutboxRelayLogger(logger), task.WithOutboxRelayMetrics(rec), task.WithOutboxRelayTracing(tracer))
	if err := rec.RegisterOutboxGauge(func(ctx context.Context) (int64, float64, error) {
		n, oldest, berr := outboxRepo.OutboxBacklog(ctx)
		if berr != nil {
			return 0, 0, berr
		}
		var age float64
		if n > 0 && !oldest.IsZero() {
			age = time.Since(oldest).Seconds()
		}
		return n, age, nil
	}); err != nil {
		level.Error(logger).Log("msg", "failed to register outbox gauge", "err", err)
	}
	if sc, ok := taskRepo.(task.StatusCounter); ok {
		if err := rec.RegisterTasksByStatusGauge(func(ctx context.Context) (map[string]int64, error) {
			counts, cerr := sc.CountByStatus(ctx)
			if cerr != nil {
				return nil, cerr
			}
			out := make(map[string]int64, len(task.AllStatuses))
			for _, s := range task.AllStatuses {
				out[string(s)] = counts[s]
			}
			return out, nil
		}); err != nil {
			level.Error(logger).Log("msg", "failed to register tasks-by-status gauge", "err", err)
		}
	}
	secret := []byte(cfg.TokenHMACSecret)
	tokenStore := task.NewTokenStore(
		inmemcache.New[string, storage.TokenMetadata](5*time.Minute),
		secret,
	)
	taskSvc := task.NewService(taskRepo, *taskPub, tx, outboxRepo, attemptRepo,
		task.WithTokenStore(tokenStore),
		task.WithTaskSourceStore(storageBackend),
		task.WithTaskSourcePresigner(storageBackend),
		task.WithTracing(tracer),
	)

	// Ownership API: the download worker claims every task through it, renews
	// the lease while it runs and commits the result under the generation of
	// its claim. workerID identifies this process as the owner.
	attemptSvc := task.NewAttemptService(attemptRepo, tx, outboxRepo, task.WithMetrics(rec), task.WithAttemptTracing(tracer))
	workerID := uuid.NewString()
	// Reconciler: dispatches due PENDING tasks and attempts whose lease
	// expired again. This is what recovers work after a crash.
	reconciler := task.NewReconciler(reconcileRepo, taskRepo, outboxRepo, task.WithReconcilerLogger(logger), task.WithReconcilerMetrics(rec))

	// Lifecycle events are notifications only; the task row is written
	// through the ownership API.
	taskEventConsumer := tasktransport.NewEventConsumer(taskSvc, sub, func(_ context.Context, err error) {
		level.Error(logger).Log("msg", "task event consumer error", "err", err)
	})

	downloadPub := download.NewDownloadEventPublisher(pub)
	dlSvc := download.NewService(
		storageBackend,
		downloadPub,
		download.WithStorageType(storage.TypeLocal),
		download.WithMetrics(rec),
		download.WithTracing(tracer),
		download.WithErrorHandler(func(_ context.Context, err error) {
			level.Error(logger).Log("msg", "download failed", "err", err)
		}),
		download.WithAttemptCoordinator(attemptSvc, workerID),
	)
	httpDL := downloader.NewHTTPDownloader(nil)
	ftpDL := downloader.NewFTPDownloader(0)
	btDL, btDlClose, err := downloader.NewBitTorrentDownloader(downloader.WithBitTorrentLogger(logger))
	must(err)
	dlSvc.RegisterDownloader("HTTP", httpDL)
	dlSvc.RegisterDownloader("HTTPS", httpDL)
	dlSvc.RegisterDownloader("FTP", ftpDL)
	dlSvc.RegisterDownloader("BITTORRENT", btDL)

	// Every dispatch runs as a claimed attempt and is acknowledged only once
	// its outcome is committed.
	consumer := downloadtransport.NewEventConsumer(dlSvc, sub, logger, downloadtransport.WithAttemptRunner(dlSvc), downloadtransport.WithMetrics(rec), downloadtransport.WithTracing(tracer))

	endpoints := apigateway.NewGatewayEndpoints(taskSvc, authMiddleware, authSvc)
	handler := apigateway.NewHTTPHandlerWithDownload(endpoints, logger, storageBackend, tokenStore)
	handler.Use(observability.RouteSpanNames)

	if cfg.PocketWebDir != "" {
		handler.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				assetPath := filepath.Join(cfg.PocketWebDir, r.URL.Path)
				if _, err := os.Stat(assetPath); err == nil {
					http.FileServer(http.Dir(cfg.PocketWebDir)).ServeHTTP(w, r)
					return
				}
			}
			http.ServeFile(w, r, filepath.Join(cfg.PocketWebDir, "index.html"))
		}))
	}

	var httpHandler http.Handler = handler
	httpHandler = middleware.CORSHTTPMiddleware(middleware.CORSOptions{
		AllowedOrigins:   splitCSV(cfg.CORSAllowedOrigins),
		AllowedMethods:   splitCSV(cfg.CORSAllowedMethods),
		AllowedHeaders:   splitCSV(cfg.CORSAllowedHeaders),
		ExposedHeaders:   splitCSV(cfg.CORSExposedHeaders),
		AllowCredentials: cfg.CORSAllowCredentials,
		PreflightMaxAge:  cfg.CORSPreflightMaxAge,
	})(httpHandler)
	httpHandler = middleware.RecoveryHTTPMiddleware(logger)(httpHandler)
	httpHandler = middleware.LoggingHTTPMiddleware(logger)(httpHandler)
	httpHandler = middleware.RequestIDHTTPMiddleware(httpHandler)

	httpHandler = observability.PublicHTTPHandler(httpHandler, rec, tracer)

	var g run.Group
	{
		srv := &http.Server{Addr: cfg.HTTPAddress, Handler: httpHandler}
		g.Add(func() error {
			level.Info(logger).Log("msg", "serving pocket-server API", "addr", srv.Addr)
			return srv.ListenAndServe()
		}, func(error) {
			btDlClose()
			ctxSh, cancelSh := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelSh()
			srv.Shutdown(ctxSh)
		})
	}
	{
		// /metrics queries SQLite on every scrape, so it serves on a separate
		// admin listener bound to loopback by default, not the public API port.
		metricsSrv := &http.Server{Addr: cfg.MetricsAddress, Handler: observability.MetricsMux(metricsHandler)}
		g.Add(func() error {
			level.Info(logger).Log("transport", "HTTP", "addr", cfg.MetricsAddress, "msg", "serving /metrics")
			if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		}, func(error) {
			ctxSh, cancelSh := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelSh()
			_ = metricsSrv.Shutdown(ctxSh)
		})
	}
	g.Add(func() error {
		return taskEventConsumer.Start(ctx)
	}, func(error) {})

	g.Add(func() error {
		return consumer.Start(ctx)
	}, func(error) {
		b.Close()
	})

	{
		relayCtx, cancelRelay := context.WithCancel(ctx)
		g.Add(func() error {
			return outboxRelay.Run(relayCtx)
		}, func(error) {
			cancelRelay()
		})
	}

	{
		reconcileCtx, cancelReconcile := context.WithCancel(ctx)
		g.Add(func() error {
			return reconciler.Run(reconcileCtx)
		}, func(error) {
			cancelReconcile()
		})
	}

	g.Add(func() error {
		<-ctx.Done()
		return ctx.Err()
	}, func(error) {})

	level.Info(logger).Log("exit", g.Run())
}

func runMigrations(pool *sqlitex.Pool) error {
	conn := pool.Get(context.Background())
	if conn == nil {
		return context.DeadlineExceeded
	}
	defer pool.Put(conn)

	err := sqlitex.ExecuteTransient(conn, `CREATE TABLE IF NOT EXISTS accounts (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        account_name TEXT NOT NULL
    );`, nil)
	if err != nil {
		return err
	}
	err = sqlitex.ExecuteTransient(
		conn,
		`CREATE UNIQUE INDEX IF NOT EXISTS accounts_account_name_key ON accounts (account_name);`,
		nil,
	)
	if err != nil {
		return err
	}
	err = sqlitex.ExecuteTransient(conn, `CREATE TABLE IF NOT EXISTS account_passwords (
        of_account_id INTEGER PRIMARY KEY,
        hashed_password TEXT NOT NULL
    );`, nil)
	if err != nil {
		return err
	}
	err = sqlitex.ExecuteTransient(conn, `CREATE TABLE IF NOT EXISTS tasks (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        of_account_id INTEGER NOT NULL,
        file_name TEXT NOT NULL,
        source_url TEXT NOT NULL,
        source_type TEXT NOT NULL,
        headers TEXT DEFAULT '{}',
        source_auth TEXT DEFAULT '{}',
        storage_type TEXT NOT NULL,
        storage_path TEXT NOT NULL,
        checksum_type TEXT,
        checksum_value TEXT,
        concurrency INTEGER DEFAULT 4,
        max_speed INTEGER,
        max_retries INTEGER NOT NULL DEFAULT 3,
        timeout INTEGER,
        status TEXT NOT NULL,
        progress REAL DEFAULT 0.0,
        downloaded_bytes INTEGER DEFAULT 0,
        total_bytes INTEGER DEFAULT 0,
        error_message TEXT,
        metadata TEXT DEFAULT '{}',
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        completed_at DATETIME,
        last_accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        expiration_days INTEGER DEFAULT 30
    );`, nil)
	if err != nil {
		return err
	}

	// Transactional outbox for task events (table task_outbox).
	err = tasksqlite.EnsureOutboxSchema(conn)
	if err != nil {
		return err
	}

	// Task ownership columns: owner, lease, generation and retry state.
	return tasksqlite.EnsureOwnershipSchema(conn)
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
