package commit

import (
	"os"

	"github.com/pkg/errors"
)

func isPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission)
}

func isLockError(err error) bool {
	return false
}
