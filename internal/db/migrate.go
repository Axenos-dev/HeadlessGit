package db

import (
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

func (db *DB) Migrate() error {
	goose.SetBaseFS(embedMigrations)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}

	return goose.Up(db.db, "migrations")
}
