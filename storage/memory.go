package storage

import (
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

func NewMemoryStore() backup.ObjectStore {
	return &memoryStore{
		objects: make(map[string]*proto.Object),
	}
}

type memoryStore struct {
	objects map[string]*proto.Object
}

func (m *memoryStore) Put(object *proto.Object) error {
	m.objects[string(object.Ref().Sha1)] = object
	return nil
}

func (m *memoryStore) Get(ref *proto.Ref) (*proto.Object, error) {
	return m.objects[string(ref.Sha1)], nil
}

func (m *memoryStore) Delete(ref *proto.Ref) error {
	delete(m.objects, string(ref.Sha1))
	return nil
}

func (m *memoryStore) Walk(t proto.ObjectType, fn backup.ObjectReceiver) error {
	for ref, object := range m.objects {
		if object.Type() == t {
			err := fn(&proto.Ref{Sha1: []byte(ref)})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
