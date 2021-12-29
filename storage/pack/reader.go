package pack

import (
	"context"
	"errors"
	"log"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

type Reader struct {
	storage ArchiveStorage
	index   ArchiveIndex
}

func (r *Reader) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	_, err := r.index.LocateObject(ref)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func (r *Reader) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	location, err := r.index.LocateObject(ref)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return nil, backup.ErrNotFound
		}
		return nil, err
	}

	archive, err := openArchive(r.storage, location.Archive)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	return archive.getRaw(ctx, ref, &location.Record)
}

func (w *Writer) Walk(ctx context.Context, load bool, t proto.ObjectType, fn backup.ObjectReceiver) error {
	archives, err := w.storage.List()
	if err != nil {
		return err
	}

	var pred loadPredicate

	switch {
	case !load:
		pred = loadNone
	case load && t == proto.ObjectType_INVALID:
		pred = loadAll
	case load && t != proto.ObjectType_INVALID:
		pred = loadType(t)
	}

	for _, name := range archives {
		archive, err := openArchive(w.storage, name)
		if err != nil {
			return err
		}

		log.Printf("Reading archive: %s", archive.name)
		err = archive.foreach(pred, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
			if t == proto.ObjectType_INVALID || hdr.Type == t {
				var obj *proto.Object
				var err error

				if load {
					obj, err = proto.NewObjectFromCompressedBytes(bytes)
					if err != nil {
						return err
					}
				}

				return fn(obj)
			}

			return nil
		})

		if err != nil {
			return err
		}
	}

	return nil
}
