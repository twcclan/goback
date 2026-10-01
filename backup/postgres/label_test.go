package postgres

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// as Postgres 17 wrote them
const (
	backupLabel = `START WAL LOCATION: 0/7000028 (file 000000010000000000000007)
CHECKPOINT LOCATION: 0/7000080
BACKUP METHOD: streamed
BACKUP FROM: primary
START TIME: 2026-10-01 19:19:45 UTC
LABEL: pg_basebackup base backup
START TIMELINE: 1
`
	backupManifest = `{ "PostgreSQL-Backup-Manifest-Version": 2,
"System-Identifier": 7691780990089482290,
"Files": [],
"WAL-Ranges": [
{ "Timeline": 1, "Start-LSN": "0/7000028", "End-LSN": "0/7000120" }
],
"Manifest-Checksum": "0"}
`
)

func TestABaseBackupDescribesItself(t *testing.T) {
	var info BaseInfo

	require.NoError(t, info.readBackupLabel(strings.NewReader(backupLabel)))
	require.NoError(t, info.readManifest(strings.NewReader(backupManifest)))

	control := make([]byte, 296)
	binary.LittleEndian.PutUint64(control, 7691780990089482290)
	require.NoError(t, info.readControl(bytes.NewReader(control)))

	require.Equal(t, BaseInfo{
		SystemID:     7691780990089482290,
		Timeline:     1,
		StartLSN:     0x7000028,
		StopLSN:      0x7000120,
		StartWALFile: "000000010000000000000007",
	}, info)

	require.Equal(t, map[string]string{
		MetaSystemID:     "7691780990089482290",
		MetaTimeline:     "1",
		MetaStartLSN:     "0/7000028",
		MetaStopLSN:      "0/7000120",
		MetaStartWALFile: "000000010000000000000007",
	}, info.Metadata())
}

func TestABackupLabelWithoutAStartIsRefused(t *testing.T) {
	var info BaseInfo
	require.Error(t, info.readBackupLabel(strings.NewReader("START TIMELINE: 1\n")))
}
