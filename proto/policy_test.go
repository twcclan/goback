package proto

import (
	"testing"

	"github.com/stretchr/testify/require"
	pb "google.golang.org/protobuf/proto"
)

func TestPolicyRoundTripsEveryScope(t *testing.T) {
	cases := map[string]*Policy{
		"store": {Sequence: 3, Scope: &Policy_Store{Store: &StoreScope{
			WritePolicy: `{"encryption":"required"}`, WritePolicyVersion: 2, KeyAcknowledgedAtNs: 5,
			DefaultRetention: `{"keep_last":3}`, HoldDays: 14, TrashDays: 7,
		}}},
		"default store": {Sequence: 1, Scope: &Policy_Store{Store: &StoreScope{}}},
		"set": {Sequence: 9, Scope: &Policy_Set{Set: &SetScope{
			SetId: 4, Name: "world", Retention: `{"keep_last":1}`, RetentionPaused: true, State: SetState_SET_CLOSING, Erase: true,
		}}},
		"commit": {Sequence: 12, Scope: &Policy_Commit{Commit: &CommitScope{Commit: refOf("commit"), DeletedAtNs: 77}}},
	}

	for name, policy := range cases {
		t.Run(name, func(t *testing.T) {
			obj := NewObject(policy)
			require.Equal(t, ObjectType_POLICY, obj.Type())

			payload, err := obj.Canonical()
			require.NoError(t, err)

			standard, err := pb.MarshalOptions{Deterministic: true}.Marshal(policy)
			require.NoError(t, err)
			require.Equal(t, standard, payload)

			decoded, err := NewObjectFromPayload(payload, ObjectType_POLICY)
			require.NoError(t, err)
			require.True(t, pb.Equal(policy, decoded.GetPolicy()))
			require.Equal(t, obj.Ref(), decoded.Ref())
		})
	}
}

func TestPolicyRefusesAnIncompleteScope(t *testing.T) {
	require.Error(t, NewObject(&Policy{Sequence: 1}).Validate())
	require.Error(t, NewObject(&Policy{Scope: &Policy_Set{Set: &SetScope{Name: "world"}}}).Validate())
	require.Error(t, NewObject(&Policy{Scope: &Policy_Commit{Commit: &CommitScope{DeletedAtNs: 1}}}).Validate())
}
