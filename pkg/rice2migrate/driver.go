package rice2migrate

import (
	"io"
	"os"

	"github.com/pkg/errors"

	"github.com/GeertJohan/go.rice"
	"github.com/golang-migrate/migrate/v4/source"
)

func init() {
	source.Register("rice", &Rice{})
}

var ErrNotImplemented = errors.New("not implemented")

type Rice struct {
	migrations *source.Migrations
	box        *rice.Box
}

func WithInstance(box *rice.Box) (source.Driver, error) {
	r := &Rice{
		migrations: source.NewMigrations(),
		box:        box,
	}

	err := r.box.Walk("/", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		m, err := source.DefaultParse(p)
		if err != nil {
			// we're going to ignore names that we cannot parse
			return nil
		}

		if !r.migrations.Append(m) {
			return errors.Errorf("unable to parse file %s", p)
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "walking rice box")
	}

	return r, nil

}

func (r *Rice) Open(url string) (source.Driver, error) {
	return nil, ErrNotImplemented
}

func (r *Rice) Close() error {
	return nil
}

func (r *Rice) First() (version uint, err error) {
	v, ok := r.migrations.First()
	if !ok {
		return 0, os.ErrNotExist
	}
	return v, nil
}

func (r *Rice) Prev(version uint) (prevVersion uint, err error) {
	v, ok := r.migrations.Prev(version)
	if !ok {
		return 0, os.ErrNotExist
	}
	return v, nil
}

func (r *Rice) Next(version uint) (nextVersion uint, err error) {
	v, ok := r.migrations.Next(version)
	if !ok {
		return 0, os.ErrNotExist
	}
	return v, nil
}

func (r *Rice) ReadUp(version uint) (io.ReadCloser, string, error) {
	if m, ok := r.migrations.Up(version); ok {
		return r.open(m)
	}
	return nil, "", os.ErrNotExist
}

func (r *Rice) ReadDown(version uint) (io.ReadCloser, string, error) {
	if m, ok := r.migrations.Down(version); ok {
		return r.open(m)
	}
	return nil, "", os.ErrNotExist
}

func (r *Rice) open(m *source.Migration) (io.ReadCloser, string, error) {
	file, err := r.box.Open(m.Raw)
	if err != nil {
		return nil, "", err
	}

	return file, m.Identifier, nil
}
