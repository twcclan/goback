package cache

import (
	"context"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/wrapped"
)

var _ backup.ObjectStore = (*Store)(nil)
var _ wrapped.Wrapper = (*Store)(nil)

func cacheable(obj *proto.Object) bool {
	if obj == nil {
		return false
	}

	switch obj.Type() {
	case proto.ObjectType_COMMIT, proto.ObjectType_TREE, proto.ObjectType_FILE:
		return true
	}

	return false
}

func New(cache backup.ObjectStore, wrapped backup.ObjectStore) *Store {
	return &Store{
		cache:   cache,
		wrapped: wrapped,
		test:    cacheable,
	}
}

type Store struct {
	cache   backup.ObjectStore
	wrapped backup.ObjectStore
	test    func(object *proto.Object) bool
}

func (s *Store) Unwrap() backup.ObjectStore { return s.wrapped }

func (s *Store) Put(ctx context.Context, object *proto.Object) error {
	err := s.wrapped.Put(ctx, object)

	if err == nil && s.test(object) {
		_ = s.cache.Put(ctx, object)
	}

	return err
}

func (s *Store) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	obj, _ := s.cache.Get(ctx, ref)

	if obj != nil {
		return obj, nil
	}

	return s.wrapped.Get(ctx, ref)
}

func (s *Store) Delete(ctx context.Context, ref *proto.Ref) error {
	err := s.wrapped.Delete(ctx, ref)
	if err == nil {
		_ = s.cache.Delete(ctx, ref)
	}

	return err
}

func (s *Store) Walk(ctx context.Context, b bool, objectType proto.ObjectType, receiver backup.ObjectReceiver) error {
	return s.wrapped.Walk(ctx, b, objectType, receiver)
}

func (s *Store) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	has, err := s.cache.Has(ctx, ref)
	if err == nil && has {
		return has, err
	}

	return s.wrapped.Has(ctx, ref)
}
