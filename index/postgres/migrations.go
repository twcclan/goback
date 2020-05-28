package postgres

import (
	"log"

	"github.com/twcclan/goback/pkg/rice2migrate"

	rice "github.com/GeertJohan/go.rice"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/pkg/errors"
)

//go:generate go run github.com/GeertJohan/go.rice/rice embed-go

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
	box, err := rice.FindBox("migrations")
	if err != nil {
		return errors.Wrap(err, "loading migration files")
	}

	source, err := rice2migrate.WithInstance(box)
	if err != nil {
		return errors.Wrap(err, "parsing migration files")
	}

	m, err := migrate.NewWithSourceInstance("rice", source, databaseURL)
	if err != nil {
		return errors.Wrap(err, "preparing migrations")
	}

	m.Log = &Log{true}

	return errors.Wrap(m.Up(), "running migrations")
}
