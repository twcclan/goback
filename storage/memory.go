package storage

import (
	"sync"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/rcrowley/go-metrics"
)

func NewMemoryStore() backup.ObjectStore {
	return &memoryStore{
		store:        make(map[string]*proto.Object),
		objects:      metrics.NewRegisteredCounter("objects", metrics.DefaultRegistry),
		objectBytes:  metrics.NewRegisteredCounter("objectBytes", metrics.DefaultRegistry),
		metaObjects:  metrics.NewRegisteredCounter("metaObjects", metrics.DefaultRegistry),
		metaBytes:    metrics.NewRegisteredCounter("metaBytes", metrics.DefaultRegistry),
		snapshotSize: metrics.NewRegisteredGauge("snapshotSize", metrics.DefaultRegistry),
	}
}

type memoryStore struct {
	store map[string]*proto.Object
	mtx   sync.Mutex

	//stats
	objects      metrics.Counter
	objectBytes  metrics.Counter
	metaObjects  metrics.Counter
	metaBytes    metrics.Counter
	snapshotSize metrics.Gauge
}

func (m *memoryStore) Put(obj *proto.Object) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	size := int64(len(obj.Bytes()))

	m.objectBytes.Inc(size)
	m.objects.Inc(1)

	if obj.Type() < proto.ObjectType_BLOB {
		m.metaBytes.Inc(size)
		m.metaObjects.Inc(1)
	}

	m.store[string(obj.Ref().Sha1)] = obj
	return nil
}

func (m *memoryStore) Get(ref *proto.Ref) (*proto.Object, error) {
	return m.store[string(ref.Sha1)], nil
}

func (m *memoryStore) Delete(ref *proto.Ref) error {
	delete(m.store, string(ref.Sha1))
	return nil
}

func (m *memoryStore) Walk(t proto.ObjectType, fn backup.ObjectReceiver) error {
	for _, object := range m.store {
		if t == proto.ObjectType_INVALID || object.Type() == t {
			err := fn(object)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *memoryStore) Close() error {
	return nil
}
