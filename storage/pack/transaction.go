package pack

import (
	"context"
	"fmt"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

var _ backup.Transaction = (*packTx)(nil)

type packTx struct {
	store  *Store
	writer *Writer
	reader *Reader
}

func (p *packTx) Put(ctx context.Context, object *proto.Object) error {
	return p.writer.Put(ctx, object)
}

func (p *packTx) Prepare(ctx context.Context) error {
	// close the tx writer to prepare for commit
	err := p.writer.Close()
	if err != nil {
		return fmt.Errorf("failed to close transaction writer: %w", err)
	}

	return nil
}

func (p *packTx) Commit(ctx context.Context) error {
	return p.store.MergeFrom(ctx, p.reader)
}

func (p *packTx) Rollback() error {
	err := p.writer.Close()
	if err != nil {
		return err
	}

	return p.writer.storage.DeleteAll()
}

type nopIndexer struct{}

func (n *nopIndexer) IndexArchive(string, IndexFile) error { return nil }

type nopLocator struct{}

func (n *nopLocator) LocateObject(*proto.Ref, ...string) (IndexLocation, error) {
	return IndexLocation{}, nil
}
