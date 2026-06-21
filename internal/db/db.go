package db

import (
	"database/sql"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	_ "modernc.org/sqlite"
)

type DB struct {
	*gen.Queries
	db *sql.DB
}

func Open(url string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", url+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}

	return &DB{
		db: sqlDB,
	}, nil
}

func (db *DB) Close() error {
	return db.db.Close()
}
