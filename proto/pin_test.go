package proto

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCanonicalGoldenVectorsReceipt(t *testing.T) {
	tree := refOf("tree")

	cases := []struct {
		name    string
		object  *Object
		payload string
	}{
		{
			name:    "stamped commit",
			object:  NewObject(&Commit{Timestamp: 1700000000, Tree: tree, BackupSet: "world", AgentId: "agent-1", ScanStartNs: 1700000000000000000, Partial: true, PolicyVersion: 1, SetId: 7, ReceivedAtNs: 5, Metadata: map[string]string{"env": "prod", "a": "b"}}),
			payload: "0880e2cfaa0612220a20dc9c5edb8b2d479e697b4b0b8ab874f32b325138598ce9e7b759eb82921106221a05776f726c642a076167656e742d31308080a8b1e39fe7cb1738014801500758056a060a01611201626a0b0a03656e76120470726f64",
		},
		{
			name:    "pin",
			object:  NewObject(&Pin{Target: tree, ReceivedAtNs: 5, Metadata: map[string]string{"k": ""}}),
			payload: "0a220a20dc9c5edb8b2d479e697b4b0b8ab874f32b325138598ce9e7b759eb8292110622180522030a016b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := tc.object.Canonical()
			require.NoError(t, err)
			require.Equal(t, tc.payload, hex.EncodeToString(payload))

			decoded, err := NewObjectFromPayload(payload, tc.object.Type())
			require.NoError(t, err)

			again, err := decoded.Canonical()
			require.NoError(t, err)
			require.Equal(t, payload, again)
		})
	}
}

func TestPinFraming(t *testing.T) {
	pin := NewObject(&Pin{Target: refOf("tree"), ReceivedAtNs: 5, Metadata: map[string]string{"k": ""}})
	require.Equal(t, ObjectType_PIN, pin.Type())

	payload, err := pin.Canonical()
	require.NoError(t, err)

	want := sha256.Sum256(append([]byte("pin "+strconv.Itoa(len(payload))+"\x00"), payload...))
	require.Len(t, payload, 43)
	require.Equal(t, want[:], pin.Ref().Hash)

	require.Error(t, NewObject(&Pin{}).Validate(), "a pin needs a target")
	require.Error(t, NewObject(&Pin{Target: refOf("tree"), Metadata: map[string]string{"": "x"}}).Validate(), "metadata keys are not empty")
	require.Error(t, NewObject(&Pin{Target: &Ref{Hash: []byte("short")}}).Validate())
}

func TestStampAndHeaderTimestamp(t *testing.T) {
	commit := NewObject(&Commit{Timestamp: 1, Tree: refOf("tree"), BackupSet: "world"})
	require.Zero(t, commit.ReceivedAtNs())

	hdr, _, err := HeaderFor(commit)
	require.NoError(t, err)
	require.Nil(t, hdr.Timestamp, "an unstamped commit gets the archive's write time")

	before := commit.Ref()
	at := time.Unix(1700000000, 42)
	commit.Stamp(7, at)
	require.EqualValues(t, 7, commit.GetCommit().SetId)
	require.Equal(t, at.UnixNano(), commit.ReceivedAtNs())
	require.False(t, commit.Ref().Equal(before), "the receipt time is in the hashed body")

	hdr, _, err = HeaderFor(commit)
	require.NoError(t, err)
	require.True(t, hdr.Timestamp.AsTime().Equal(at), "the header carries the receipt time so a rebuild reproduces it")

	pin := NewObject(&Pin{Target: refOf("tree")})
	pin.Stamp(9, at)
	require.Equal(t, at.UnixNano(), pin.ReceivedAtNs())

	blob := NewObject(&Blob{Data: []byte("x")})
	blob.Stamp(1, at)
	require.Zero(t, blob.ReceivedAtNs())
}
