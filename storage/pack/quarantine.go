package pack

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// RetiredExt marks an archive a rewrite retired: <archive>.retired names
// the day it was retired. The store no longer loads the archive, a
// collection deletes it once the quarantine period has passed, and
// RestoreQuarantined puts it back until then.
const RetiredExt = ".retired"

const quarantineDay = "2006-01-02"

// DefaultQuarantine is how long a retired archive is kept by default, so
// reads already under way, in this process or another, finish against it.
const DefaultQuarantine = 24 * time.Hour

// archiveNames lists the archives in the storage that are not retired.
func (ps *PackStorage) archiveNames() ([]string, error) {
	matches, err := ps.storage.List(ArchiveSuffix)
	if err != nil {
		return nil, err
	}

	retired, err := ps.markerIDs(RetiredExt)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if name := strings.TrimSuffix(match, ArchiveSuffix); !retired[name] {
			names = append(names, name)
		}
	}

	return names, nil
}

// quarantineArchive marks an archive retired, keeping its files for the
// quarantine period.
func (ps *PackStorage) quarantineArchive(name string, now time.Time) error {
	err := ps.storage.CreateNew(name+RetiredExt, []byte(now.UTC().Format(quarantineDay)))
	if err != nil && !errors.Is(err, ErrFileExists) {
		return fmt.Errorf("retiring %s: %w", name, err)
	}

	return nil
}

// PurgeQuarantine deletes the archives retired longer than period ago and
// returns how many it deleted.
func (ps *PackStorage) PurgeQuarantine(period time.Duration, now time.Time) (int, error) {
	retired, err := ps.markerIDs(RetiredExt)
	if err != nil {
		return 0, err
	}

	purged := 0

	for name := range retired {
		day, err := ps.retiredOn(name)
		if err != nil {
			ps.logger.Warn("reading when an archive was retired failed", "archive", name, "err", err)
			continue
		}

		if now.Sub(day) < period+24*time.Hour {
			continue
		}

		ps.deleteArchiveFiles(name)

		if err := ps.storage.Delete(name + RetiredExt); err != nil && !notExist(err) {
			return purged, err
		}

		purged++
	}

	return purged, nil
}

// retiredBytes sums the sizes of the retired archives still stored.
func (ps *PackStorage) retiredBytes() (uint64, error) {
	retired, err := ps.markerIDs(RetiredExt)
	if err != nil {
		return 0, err
	}

	var total uint64

	for name := range retired {
		file, err := ps.storage.Open(name + ArchiveSuffix)
		if notExist(err) {
			continue
		}

		if err != nil {
			return 0, err
		}

		info, err := file.Stat()
		_ = file.Close()

		if err != nil {
			return 0, err
		}

		total += uint64(info.Size())
	}

	return total, nil
}

func (ps *PackStorage) retiredOn(name string) (time.Time, error) {
	file, err := ps.storage.Open(name + RetiredExt)
	if err != nil {
		return time.Time{}, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return time.Time{}, err
	}

	return time.Parse(quarantineDay, string(data))
}

// ErrNotQuarantined is RestoreQuarantined's answer for an archive the
// quarantine does not hold.
var ErrNotQuarantined = errors.New("the quarantine holds no such archive")

// RestoreQuarantined puts a retired archive back, where the store picks
// it up as committed.
func (ps *PackStorage) RestoreQuarantined(name string) error {
	if !ps.hasIndexFile(name) {
		return fmt.Errorf("%w: %s", ErrNotQuarantined, name)
	}

	err := ps.storage.Delete(name + RetiredExt)
	if notExist(err) {
		return fmt.Errorf("%w: %s", ErrNotQuarantined, name)
	}

	if err != nil {
		return err
	}

	ps.mtx.Lock()
	delete(ps.retired, name)
	ps.mtx.Unlock()

	return ps.refreshArchives()
}
