package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	_ "modernc.org/sqlite"
)

type DB struct {
	*gen.Queries
	db *sql.DB
}

func Open(url string) (*DB, error) {
	// sqlite won't create the file's parent directory; make sure it exists.
	if dir := filepath.Dir(url); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
	}

	sqlDB, err := sql.Open("sqlite", url+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}

	return &DB{
		Queries: gen.New(sqlDB),
		db:      sqlDB,
	}, nil
}

func (db *DB) Close() error {
	return db.db.Close()
}

func (db *DB) Health(ctx context.Context) error {
	return db.db.PingContext(ctx)
}
