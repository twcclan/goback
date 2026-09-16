//go:build windows

package backup

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// IsPermissionError reports whether an open or list failed for lack of access.
func IsPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_CANT_ACCESS_FILE)
}

// IsLockError reports whether a read failed because another process holds
// a byte range lock.
func IsLockError(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// OwnerNames is empty on Windows: there is no uid or gid to record.
func OwnerNames(os.FileInfo) (string, string) {
	return "", ""
}

// FileIdentity has no ctime or inode on Windows without opening the file.
func FileIdentity(os.FileInfo) (int64, uint64) {
	return 0, 0
}
