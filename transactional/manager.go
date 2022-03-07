package transactional

import (
	"context"
	"log"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

type TransactionStore interface {
	NewTransaction(ctx context.Context) (*proto.Transaction, error)
	SaveTransaction(ctx context.Context, tx *proto.Transaction) error
	FindTransaction(ctx context.Context, id string) (*proto.Transaction, error)
}

func New(objectStore backup.Transactioner, txStore TransactionStore) (*Manager, error) {
	return &Manager{
		objectStore: objectStore,
		txStore:     txStore,
	}, nil
}

type Manager struct {
	objectStore backup.Transactioner
	txStore     TransactionStore
	async       bool
}

// Transaction begins a new transaction on the object store and keeps track of it in a separate transaction store.
//	The callback fn will be called within the context of a transaction. If the callback returns an error, the transaction
//	will be rolled back. If a nil-error is returned by the callback, the transaction will be prepared for commit, which
//	ensures that the data is safely stored. Committing the transaction to the underlying store might happen asynchronously.
func (m *Manager) Transaction(ctx context.Context, fn func(transaction backup.Transaction) error) (*proto.Transaction, error) {
	tx, err := m.txStore.NewTransaction(ctx)
	if err != nil {
		return nil, err
	}

	storeTx, err := m.objectStore.Transaction(ctx, tx)
	if err != nil {
		return nil, err
	}

	err = fn(storeTx)
	if err != nil {
		// if there was an error, we need to rollback the transaction
		rollbackErr := m.changeTransactionStatus(ctx, tx, proto.Transaction_ABORTED, func() error {
			return storeTx.Rollback()
		})
		if rollbackErr != nil {
			// at this point, things are looking pretty bad and we are probably left in a weird state.
			// TODO: there needs to be a process to clean these up
			log.Printf("failed rolling back transaction: %v", rollbackErr)
		}

		return nil, err
	}

	err = m.changeTransactionStatus(ctx, tx, proto.Transaction_PREPARED, func() error {
		return storeTx.Prepare(ctx)
	})
	if err != nil {
		return tx, err
	}

	return tx, nil
}

func (m *Manager) CommitTransaction(ctx context.Context, id string) (*proto.Transaction, error) {
	tx, err := m.txStore.FindTransaction(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := assertTxStatus(tx, proto.Transaction_PREPARED); err != nil {
		return nil, err
	}

	storeTx, err := m.objectStore.Transaction(ctx, tx)
	if err != nil {
		return nil, err
	}

	return tx, m.changeTransactionStatus(ctx, tx, proto.Transaction_COMMITTED, func() error {
		return storeTx.Commit(ctx)
	})
}

func (m *Manager) changeTransactionStatus(ctx context.Context, tx *proto.Transaction, to proto.Transaction_Status, via func() error) error {
	// do nothing if the transaction is already in the desired state
	if tx.GetStatus() == to {
		return nil
	}

	if via != nil {
		// call the function that does the work and update the transaction status afterwards
		err := via()
		if err != nil {
			return err
		}
	}

	orig := tx.GetStatus()
	tx.Status = to
	err := m.txStore.SaveTransaction(ctx, tx)
	if err != nil {
		// reset to the previous status, if we could not save
		tx.Status = orig
		return err
	}

	return nil
}
