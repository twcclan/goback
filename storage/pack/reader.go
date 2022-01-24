package pack

import (
	"context"
	"errors"
	"log"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

type objectLocator interface {
	LocateObject(ref *proto.Ref, exclude ...string) (IndexLocation, error)
}

func NewReader(storage ArchiveStorage, locator objectLocator) *Reader {
	return &Reader{
		storage: storage,
		locator: locator,
	}
}

type Reader struct {
	storage ArchiveStorage
	locator objectLocator
}

func (r *Reader) open(name string) (*archiveReader, error) {
	file, err := r.storage.Open(archiveFileName(name))
	if err != nil {
		return nil, err
	}

	return readArchive(file)
}

func (r *Reader) Has(_ context.Context, ref *proto.Ref) (bool, error) {
	_, err := r.locator.LocateObject(ref)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func (r *Reader) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	location, err := r.locator.LocateObject(ref)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return nil, backup.ErrNotFound
		}
		return nil, err
	}

	archive, err := r.open(location.Archive)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	return archive.getRaw(ctx, ref, &location.Record)
}

func (r *Reader) WriteTo(ctx context.Context, w *Writer) error {
	archives, err := r.storage.List(ArchiveSuffix)
	if err != nil {
		return err
	}

	for _, name := range archives {
		file, err := r.storage.Open(name)
		if err != nil {
			return err
		}

		err = streamArchive(file, loadAll, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
			return w.putRaw(ctx, hdr, bytes)
		})

		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Reader) Walk(ctx context.Context, load bool, t proto.ObjectType, fn backup.ObjectReceiver) error {
	archives, err := r.storage.List(ArchiveSuffix)
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
		file, err := r.storage.Open(name)
		if err != nil {
			return err
		}

		log.Printf("Reading archiveWriter: %s", name)
		err = streamArchive(file, pred, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
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
