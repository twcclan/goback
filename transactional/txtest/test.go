package txtest

import (
	"context"
	"testing"
	"time"

	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/transactional"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTransactionStore(t *testing.T, store transactional.TransactionStore) {
	type test struct {
		name string
		fn   func(t *testing.T, store transactional.TransactionStore)
	}

	tests := []test{
		{"new transaction", testNewTransaction},
		{"find transaction", testFindTransaction},
		{"update transaction", testUpdateTransaction},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.fn(t, store)
		})
	}
}

func assertTimestampCloseEnough(t *testing.T, expected, actual *timestamppb.Timestamp) {
	t.Helper()

	require.Equal(t, expected.Seconds, actual.Seconds, "seconds should be equal")
	require.InDelta(t, expected.Nanos, actual.Nanos, float64(10*time.Microsecond))
}

func testNewTransaction(t *testing.T, store transactional.TransactionStore) {
	tx, err := store.NewTransaction(context.Background())
	require.Nil(t, err, "new transaction should succeed")
	require.NotNil(t, tx)

	require.Equal(t, proto.Transaction_OPEN, tx.GetStatus(), "transaction should start in open state")
	require.NotEmpty(t, tx.GetTransactionId())
	require.NotNil(t, tx.GetCreatedAt())
	require.NotNil(t, tx.GetUpdatedAt())
}

func testFindTransaction(t *testing.T, store transactional.TransactionStore) {
	tx, err := store.NewTransaction(context.Background())
	require.Nil(t, err)

	dbTx, err := store.FindTransaction(context.Background(), tx.GetTransactionId())
	require.Nil(t, err)

	assertTimestampCloseEnough(t, tx.CreatedAt, dbTx.CreatedAt)
	assertTimestampCloseEnough(t, tx.UpdatedAt, dbTx.UpdatedAt)

	// unset all the date fields so that we can compare better
	tx.CreatedAt = nil
	tx.UpdatedAt = nil
	dbTx.UpdatedAt = nil
	dbTx.CreatedAt = nil

	require.Equal(t, tx, dbTx)
}

func testUpdateTransaction(t *testing.T, store transactional.TransactionStore) {
	values := []proto.Transaction_Status{
		proto.Transaction_INVALID,
		proto.Transaction_OPEN,
		proto.Transaction_PREPARED,
		proto.Transaction_COMMITTED,
		proto.Transaction_ABORTED,
	}

	for _, status := range values {
		t.Run(status.String(), func(t *testing.T) {
			tx, err := store.NewTransaction(context.Background())
			require.Nil(t, err)

			tx.Status = status
			updated := tx.GetUpdatedAt()
			created := tx.GetCreatedAt()

			err = store.SaveTransaction(context.Background(), tx)
			require.Nil(t, err)

			require.Equal(t, status, tx.GetStatus())
			require.NotEqual(t, updated, tx.GetUpdatedAt())
			assertTimestampCloseEnough(t, created, tx.GetCreatedAt())

			dbTx, err := store.FindTransaction(context.Background(), tx.TransactionId)
			require.Nil(t, err)

			require.Equal(t, status, dbTx.GetStatus())
			require.NotEqual(t, updated, dbTx.GetUpdatedAt())

			assertTimestampCloseEnough(t, created, dbTx.GetCreatedAt())
			assertTimestampCloseEnough(t, tx.GetUpdatedAt(), dbTx.GetUpdatedAt())
		})
	}
}
