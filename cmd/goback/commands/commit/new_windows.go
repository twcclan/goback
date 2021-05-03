package commit

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func isPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_CANT_ACCESS_FILE)
}

func isLockError(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
