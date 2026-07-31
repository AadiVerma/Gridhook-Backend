package db

import (
	"context"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"gridhook.dev/connector-backend/internal/config"
)

type DB struct {
	*gorm.DB
}

func Connect(ctx context.Context, cfg config.Database) (*DB, error) {
	gdb, err := gorm.Open(postgres.Open(cfg.URL), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),

		// Turns a raw pg 23505/23503 into gorm.ErrDuplicatedKey /
		// gorm.ErrForeignKeyViolated so a service can map a constraint breach to
		// ErrConflict instead of letting it fall through as a 500. Inert
		// everywhere that doesn't test for those sentinels.
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("db: underlying sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {

		_ = sqlDB.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return &DB{DB: gdb}, nil
}

func (d *DB) Ping(ctx context.Context) error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return fmt.Errorf("db: underlying sql.DB: %w", err)
	}
	return sqlDB.PingContext(ctx)
}

func (d *DB) Close() error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return fmt.Errorf("db: underlying sql.DB: %w", err)
	}
	return sqlDB.Close()
}
