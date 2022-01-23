package pack

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/twcclan/goback/proto"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type writeFile interface {
	io.WriteCloser
}

func writeArchive(file File, name string) (*archiveWriter, error) {
	a := &archiveWriter{
		file:       file,
		name:       name,
		writeIndex: map[string]*IndexRecord{},
	}

	return a, nil
}

type archiveWriter struct {
	file       writeFile
	name       string
	size       uint64
	writeIndex map[string]*IndexRecord

	mtx  sync.RWMutex
	last *proto.Ref
}

func (a *archiveWriter) Put(ctx context.Context, object *proto.Object) error {
	ctx, span := trace.StartSpan(ctx, "archiveWriter.Put")
	defer span.End()

	bytes := object.CompressedBytes()
	ref := object.Ref()

	hdr := &proto.ObjectHeader{
		Compression: proto.Compression_GZIP,
		Ref:         ref,
		Type:        object.Type(),
	}

	return a.putRaw(ctx, hdr, bytes)
}

func (a *archiveWriter) putTombstone(ctx context.Context, ref *proto.Ref) error {
	tombstoneSha := sha1.Sum(ref.Sha1)
	tombstoneRef := &proto.Ref{Sha1: tombstoneSha[:]}

	hdr := &proto.ObjectHeader{
		Ref:          tombstoneRef,
		TombstoneFor: ref,
		Type:         proto.ObjectType_TOMBSTONE,
	}

	return a.putRaw(ctx, hdr, nil)
}

func (a *archiveWriter) putRaw(ctx context.Context, hdr *proto.ObjectHeader, bytes []byte) error {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	ref := hdr.Ref
	// make sure not to have duplicates within a single file
	if _, ok := a.writeIndex[string(ref.Sha1)]; ok {
		return nil
	}

	// add type info where it isn't already present
	if hdr.Type == proto.ObjectType_INVALID {
		obj, err := proto.NewObjectFromCompressedBytes(bytes)
		if err != nil {
			return err
		}

		hdr.Type = obj.Type()
	}

	hdr.Predecessor = a.last
	hdr.Size = uint64(len(bytes))

	// keep the timestamp if it's already present. this is important,
	// because we rely on the information of when a certain object
	// first entered our system
	if hdr.Timestamp == nil {
		hdr.Timestamp = timestamppb.Now()
	}

	hdrBytes := proto.Bytes(hdr)
	hdrBytesSize := uint64(len(hdrBytes))

	// construct a buffer with our header preceeded by a varint describing its size
	hdrBytes = append(proto.EncodeVarint(nil, hdrBytesSize), hdrBytes...)

	// add our payload
	data := append(hdrBytes, bytes...)

	start := time.Now()
	_, err := a.file.Write(data)
	if err != nil {
		return fmt.Errorf("failed writing header: %w", err)
	}

	writeLatency := float64(time.Since(start)) / float64(time.Millisecond)

	record := &IndexRecord{
		Offset: uint32(a.size),
		Length: uint32(len(hdrBytes) + len(bytes)),
		Type:   uint32(hdr.Type),
	}

	copy(record.Sum[:], ref.Sha1)

	a.writeIndex[string(ref.Sha1)] = record

	a.size += uint64(record.Length)
	a.last = ref

	if ctx, err := tag.New(ctx,
		tag.Insert(KeyObjectType, hdr.Type.String()),
	); err == nil {
		stats.Record(ctx,
			PutObjectSize.M(int64(hdr.Size)),
			ArchiveWriteLatency.M(writeLatency),
			ArchiveWriteSize.M(int64(len(data))),
		)
	}

	return nil
}

func (a *archiveWriter) getIndex() IndexFile {
	idx := make(IndexFile, 0, len(a.writeIndex))

	for _, loc := range a.writeIndex {
		idx = append(idx, *loc)
	}

	sort.Sort(idx)

	return idx
}

func (a *archiveWriter) Close() error {
	return a.file.Close()
}
