package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	kitlog "github.com/go-kit/log"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"github.com/rubenv/sql-migrate"

	"github.com/yuisofull/goload/internal/configs"
)

//go:embed ../../migrations/mysql/*.sql
var migrationDirectoryMySQL embed.FS

type migrator struct {
	db *sql.DB
}

func (m migrator) migrate(ctx context.Context, direction migrate.MigrationDirection, logger kitlog.Logger) error {
	migrationCount, err := migrate.ExecContext(ctx, m.db, "mysql", migrate.EmbedFileSystemMigrationSource{
		FileSystem: migrationDirectoryMySQL,
		Root:       "migrations/mysql",
	}, direction)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	logger.Log("msg", "Applied migrations", "count", migrationCount)
	return nil
}

func (m migrator) Up(ctx context.Context, logger kitlog.Logger) error {
	return m.migrate(ctx, migrate.Up, logger)
}

func (m migrator) Down(ctx context.Context, logger kitlog.Logger) error {
	return m.migrate(ctx, migrate.Down, logger)
}

func main() {
	var direction string
	flag.StringVar(&direction, "direction", "up", "Direction: up or down")
	flag.Parse()

	logger := kitlog.NewLogfmtLogger(os.Stderr)

	loadedConfig, err := configs.Load()
	if err != nil {
		logger.Log("msg", "Failed to load config", "err", err)
		os.Exit(1)
	}

	config := loadedConfig.MySQL

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
		config.Username, config.Password, config.Host, config.Port, config.Database)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		logger.Log("msg", "Failed to open DB", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	m := migrator{db: db}
	ctx := context.Background()

	switch direction {
	case "up":
		err = m.Up(ctx, logger)
	case "down":
		err = m.Down(ctx, logger)
	default:
		logger.Log("msg", "Invalid direction", "direction", direction)
		os.Exit(1)
	}

	if err != nil {
		logger.Log("msg", "Migration error", "err", err)
		os.Exit(1)
	}
}
