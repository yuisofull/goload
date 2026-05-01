package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/go-llsqlite/crawshaw/sqlitex"
	"github.com/oklog/run"

	"github.com/yuisofull/goload/internal/apigateway"
	auth "github.com/yuisofull/goload/internal/auth"
	authsqlite "github.com/yuisofull/goload/internal/auth/sqlite"
	"github.com/yuisofull/goload/internal/download"
	downloader "github.com/yuisofull/goload/internal/download/downloader"
	downloadtransport "github.com/yuisofull/goload/internal/download/transport"
	"github.com/yuisofull/goload/internal/storage"
	"github.com/yuisofull/goload/internal/task"
	tasksqlite "github.com/yuisofull/goload/internal/task/sqlite"
	tasktransport "github.com/yuisofull/goload/internal/task/transport"
	"github.com/yuisofull/goload/pkg/crypto/bcrypt"
	inmem "github.com/yuisofull/goload/pkg/message/inmem"
	"github.com/yuisofull/goload/pkg/middleware"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
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

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Open SQLite DB for pocket mode
	dbPath := cfg.PocketDBPath

	pool, err := sqlitex.Open(dbPath, 0, 10)
	must(err)
	defer pool.Close()

	// Run simple migrations (create tables if not exists)
	must(runMigrations(pool))

	// Initialize storage backend (local filesystem)
	dataDir := cfg.PocketDataDir
	storageBackend, err := storage.NewLocalBackend(dataDir)
	must(err)

	// Create in-memory broker
	b := inmem.NewBroker(100, logger)
	pub := inmem.NewPublisher(b)
	sub := inmem.NewSubscriber(b)

	// Initialize auth and task persistence using direct crawshaw implementations
	authStore := authsqlite.New(pool)
	taskRepo := tasksqlite.NewTaskRepo(pool)
	tx := tasksqlite.NewTxManager(pool)

	// Password hasher
	bcryptHasher := bcrypt.NewHasher(10)
	hasher := auth.NewPasswordHasher(bcryptHasher)

	// Token manager: use a no-op manager for pocket single-user mode
	tokenManager := auth.NewNoopTokenManager(24 * time.Hour)

	// Auth service
	authSvc := auth.NewService(authStore.AccountStore, authStore.AccountPasswordStore, authStore.TxManager, hasher, tokenManager)

	// Task service: use in-memory pubsub publisher
	taskPub := task.NewEventPublisher(pub)
	tokenStore := task.NewInmemTokenStore()
	taskSvc := task.NewService(taskRepo, *taskPub, tx,
		task.WithTokenStore(tokenStore),
		task.WithPresigner(storageBackend),
		task.WithTaskSourceStore(storageBackend),
		task.WithTaskSourcePresigner(storageBackend),
	)

	// Task event consumer 
	var taskEventConsumer *tasktransport.EventConsumer
	{
		taskEventConsumer = tasktransport.NewEventConsumer(taskSvc, sub)
	}


	// Download service
	downloadPub := download.NewDownloadEventPublisher(pub)
	dlSvc := download.NewService(storageBackend, downloadPub, download.WithErrorHandler(func(ctx context.Context, err error) {
		level.Error(logger).Log("msg", "download failed", "err", err)
	}))
	// register downloaders
	httpDL := downloader.NewHTTPDownloader(nil)
	ftpDL := downloader.NewFTPDownloader(0)
	btDL, btDlClose := downloader.NewBitTorrentDownloader(downloader.WithBitTorrentLogger(logger))
	dlSvc.RegisterDownloader("HTTP", httpDL)
	dlSvc.RegisterDownloader("HTTPS", httpDL)
	dlSvc.RegisterDownloader("FTP", ftpDL)
	dlSvc.RegisterDownloader("BITTORRENT", btDL)

	// Start event consumer (download service listens for task events)
	consumer := downloadtransport.NewEventConsumer(dlSvc, sub, logger)

	// Create a default pocket account and use a no-auth middleware that
	// injects this account ID into requests (single-user mode).
	var defaultAccountID uint64
	if acct, err := authStore.AccountStore.GetAccountByAccountName(ctx, "pocket"); err == nil && acct != nil {
		defaultAccountID = acct.Id
	} else {
		id, err := authStore.AccountStore.CreateAccount(ctx, &auth.Account{AccountName: "pocket"})
		if err != nil {
			// try to read it back if creation failed due to race
			acct2, err2 := authStore.AccountStore.GetAccountByAccountName(ctx, "pocket")
			must(err2)
			defaultAccountID = acct2.Id
		} else {
			// set an empty password (not used) so other code expecting a password won't fail
			hashed, err := hasher.Hash(ctx, "")
			must(err)
			_ = authStore.AccountPasswordStore.CreateAccountPassword(ctx, &auth.AccountPassword{OfAccountId: id, HashedPassword: hashed})
			defaultAccountID = id
		}
	}

	authMiddleware := apigateway.NewNoAuthMiddleware(defaultAccountID)
	endpoints := apigateway.NewGatewayEndpoints(taskSvc, authMiddleware, authSvc)
	handler := apigateway.NewHTTPHandlerWithDownload(endpoints, logger, storageBackend, tokenStore)
	if cfg.PocketWebDir != "" {
		handler.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := filepath.Join(cfg.PocketWebDir, r.URL.Path)
			_, err := os.Stat(path)
			if os.IsNotExist(err) || r.URL.Path == "/" {
				http.ServeFile(w, r, filepath.Join(cfg.PocketWebDir, "index.html"))
				return
			}
			http.FileServer(http.Dir(cfg.PocketWebDir)).ServeHTTP(w, r)
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

	var g run.Group
	{
		srv := &http.Server{Addr: ":8080", Handler: httpHandler}
		g.Add(func() error {
			level.Info(logger).Log("msg", "serving pocket-mode API", "addr", srv.Addr)
			return srv.ListenAndServe()
		}, func(error) {
			btDlClose() // ensure BitTorrent downloader is closed to release any resources
			ctxSh, cancelSh := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelSh()
			srv.Shutdown(ctxSh)
		})
	}
	g.Add(func() error {
		return taskEventConsumer.Start(ctx)
	
		}, func(error) {
	})

	g.Add(func() error {
		return consumer.Start(ctx)
	}, func(error) {
		b.Close()
	})

	g.Add(func() error {
		<-ctx.Done()
		return ctx.Err()
	}, func(error) {})

	level.Info(logger).Log("exit", g.Run())
}

// runMigrations creates minimal tables compatible with sqlc queries used by existing code.
func runMigrations(pool *sqlitex.Pool) error {
	conn := pool.Get(context.Background())
	if conn == nil {
		return context.DeadlineExceeded
	}
	defer pool.Put(conn)

	// Accounts
	err := sqlitex.ExecuteTransient(conn, `CREATE TABLE IF NOT EXISTS accounts (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        account_name TEXT NOT NULL
    );`, nil)
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
	err = sqlitex.ExecuteTransient(conn, `CREATE TABLE IF NOT EXISTS token_public_keys (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        public_key TEXT NOT NULL
    );`, nil)
	if err != nil {
		return err
	}

	// Tasks table adapted for SQLite
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
	return err
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
