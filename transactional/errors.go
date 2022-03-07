package transactional

import (
	"fmt"

	"github.com/twcclan/goback/proto"
)

type TransactionStateErr struct {
	Desired proto.Transaction_Status
	Actual  proto.Transaction_Status
}

func (t *TransactionStateErr) Error() string {
	return fmt.Sprintf("invalid transaction state: expected %s != actual %s", t.Desired, t.Actual)
}

func assertTxStatus(tx *proto.Transaction, desired proto.Transaction_Status) error {
	if tx.GetStatus() != desired {
		return &TransactionStateErr{
			Desired: desired,
			Actual:  tx.GetStatus(),
		}
	}

	return nil
}
