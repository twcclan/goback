package migrations

import (
	"fmt"
	"io/fs"
	"log"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/pkg/errors"
)

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

func Run(databaseURL string, migrations fs.FS, path string) error {
	log.Println("Looking for database migrations to run")

	source, err := iofs.New(migrations, path)
	if err != nil {
		return fmt.Errorf("failed parsing migration files: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		return fmt.Errorf("failed preparing migrations: %w", err)
	}

	m.Log = &Log{true}

	return errors.Wrap(m.Up(), "running migrations")
}
