package pack

import "time"

// Version orders the records of one object: by the bucket's creation time
// of the index file of the archive that first stored the record, then by
// the record's offset within that archive. A rewrite keeps the version of
// what it moves.
type Version struct {
	Time   time.Time
	Offset uint32
}

// Before reports whether v is older than o.
func (v Version) Before(o Version) bool {
	if !v.Time.Equal(o.Time) {
		return v.Time.Before(o.Time)
	}

	return v.Offset < o.Offset
}
