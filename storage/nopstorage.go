package storage

import (
	"os"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/rcrowley/go-metrics"
)

func NewNopStorage() *NopStorage {
	return &NopStorage{
		objects:      metrics.NewRegisteredCounter("objects", metrics.DefaultRegistry),
		objectBytes:  metrics.NewRegisteredCounter("objectBytes", metrics.DefaultRegistry),
		metaObjects:  metrics.NewRegisteredCounter("metaObjects", metrics.DefaultRegistry),
		metaBytes:    metrics.NewRegisteredCounter("metaBytes", metrics.DefaultRegistry),
		snapshotSize: metrics.NewRegisteredGauge("snapshotSize", metrics.DefaultRegistry),
	}
}

type NopStorage struct {
	objects      metrics.Counter
	objectBytes  metrics.Counter
	metaObjects  metrics.Counter
	metaBytes    metrics.Counter
	snapshotSize metrics.Gauge
}

func (n *NopStorage) Get(ref *proto.Ref) (*proto.Object, error) {
	return nil, nil
}

func (n *NopStorage) Put(obj *proto.Object) error {
	size := int64(len(obj.Bytes()))

	n.objectBytes.Inc(size)
	n.objects.Inc(1)

	if obj.Type() < proto.ObjectType_BLOB {
		n.metaBytes.Inc(size)
		n.metaObjects.Inc(1)
	}

	return nil
}

func (n *NopStorage) Delete(ref *proto.Ref) error {
	return nil
}

func (n *NopStorage) Walk(t proto.ObjectType, fn backup.ObjectReceiver) error {
	return nil
}

func (n *NopStorage) Stats() {
	metrics.WriteOnce(metrics.DefaultRegistry, os.Stdout)
}
