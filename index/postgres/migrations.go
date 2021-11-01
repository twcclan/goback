package postgres

import (
	"embed"
	"log"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/pkg/errors"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Log struct {
	verbose bool
}

func (l *Log) Printf(format string, args ...interface{}) {
	log.Printf(format, args...)
}

func (l *Log) Verbose() bool {
	return l.verbose
}

var _ migrate.Logger = (*Log)(nil)

func runMigrations(databaseURL string) error {
	log.Println("Looking for database migrations to run")

	source, err := iofs.New(migrations, "migrations")
	if err != nil {
		return errors.Wrap(err, "parsing migration files")
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		return errors.Wrap(err, "preparing migrations")
	}

	m.Log = &Log{true}

	return errors.Wrap(m.Up(), "running migrations")
}
