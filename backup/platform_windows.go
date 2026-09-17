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

// lockedElsewhere reports whether another process holds the file open
// exclusively or has locked its first byte, as a Java FileChannel lock is.
func lockedElsewhere(path string) bool {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return errors.Is(err, windows.ERROR_SHARING_VIOLATION)
	}
	defer file.Close()

	var overlapped windows.Overlapped
	handle := windows.Handle(file.Fd())

	err = windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if err != nil {
		return true
	}

	_ = windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)

	return false
}

// OwnerNames is empty on Windows: there is no uid or gid to record.
func OwnerNames(os.FileInfo) (string, string) {
	return "", ""
}

// FileIdentity has no ctime or inode on Windows without opening the file.
func FileIdentity(os.FileInfo) (int64, uint64) {
	return 0, 0
}

// entryInfo stats the path itself: the directory listing lags behind
// writes through a handle another process holds open, so a file a server
// keeps writing would look unchanged.
func entryInfo(_ os.DirEntry, path string) (os.FileInfo, error) {
	return os.Lstat(path)
}
