package storage

import (
	"os"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/rcrowley/go-metrics"
)

func NewNopStorage() *NopStorage {
	return &NopStorage{
		chunks:       metrics.NewRegisteredCounter("chunks", metrics.DefaultRegistry),
		chunkBytes:   metrics.NewRegisteredCounter("chunkBytes", metrics.DefaultRegistry),
		metaChunks:   metrics.NewRegisteredCounter("metaChunks", metrics.DefaultRegistry),
		metaBytes:    metrics.NewRegisteredCounter("metaBytes", metrics.DefaultRegistry),
		snapshotSize: metrics.NewRegisteredGauge("snapshotSize", metrics.DefaultRegistry),
	}
}

type NopStorage struct {
	chunks       metrics.Counter
	chunkBytes   metrics.Counter
	metaChunks   metrics.Counter
	metaBytes    metrics.Counter
	snapshotSize metrics.Gauge
}

func (n *NopStorage) Read(ref *proto.Ref) (*proto.Object, error) {
	return nil, nil
}

func (n *NopStorage) Create(chunk *proto.Object) error {
	n.chunkBytes.Inc(int64(proto.Size(chunk)))
	n.chunks.Inc(1)

	return nil
}

func (n *NopStorage) Delete(ref *proto.Ref) error {
	return nil
}

func (n *NopStorage) Walk(t proto.ObjectType, fn backup.ChunkWalkFn) error {
	return nil
}

func (n *NopStorage) Stats() {
	metrics.WriteOnce(metrics.DefaultRegistry, os.Stdout)
}
