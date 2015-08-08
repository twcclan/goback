package backup

import (
	"os"

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

func (n *NopStorage) Read(ref *proto.ChunkRef) (*proto.Chunk, error) {
	return nil, nil
}

func (n *NopStorage) Create(chunk *proto.Chunk) error {
	switch chunk.Ref.Type {
	case proto.ChunkType_SNAPSHOT:
		n.snapshotSize.Update(int64(len(chunk.Data)))
		fallthrough
	case proto.ChunkType_FILE_INFO, proto.ChunkType_FILE:
		n.metaBytes.Inc(int64(len(chunk.Data)))
		n.metaChunks.Inc(1)
	case proto.ChunkType_DATA:
		n.chunkBytes.Inc(int64(len(chunk.Data)))
		n.chunks.Inc(1)
	}

	return nil
}

func (n *NopStorage) Delete(ref *proto.ChunkRef) error {
	return nil
}

func (n *NopStorage) Walk(t proto.ChunkType, fn ChunkWalkFn) error {
	return nil
}

func (n *NopStorage) Stats() {
	metrics.WriteOnce(metrics.DefaultRegistry, os.Stdout)
}
