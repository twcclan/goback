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

// New layers cache over wrapped for its commits, trees and files.
func New(cache backup.ObjectStore, wrapped backup.ObjectStore) *Store {
	return &Store{
		cache:   cache,
		wrapped: wrapped,
		test:    cacheable,
	}
}

// Store serves metadata objects from cache before wrapped, filling the cache
// on reads and writes; cache failures are ignored.
type Store struct {
	cache   backup.ObjectStore
	wrapped backup.ObjectStore
	test    func(object *proto.Object) bool
}

// Unwrap implements wrapped.Wrapper.
func (s *Store) Unwrap() backup.ObjectStore { return s.wrapped }

// Put implements backup.ObjectStore.
func (s *Store) Put(ctx context.Context, object *proto.Object) error {
	err := s.wrapped.Put(ctx, object)

	if err == nil && s.test(object) {
		_ = s.cache.Put(ctx, object)
	}

	return err
}

// GetTree forwards to the wrapped store's prefetch, if it has one, and keeps
// the fetched trees so the walker's later Gets are served locally.
func (s *Store) GetTree(ctx context.Context, ref *proto.Ref, maxDepth uint32) ([]*proto.Object, error) {
	fetcher, ok := s.wrapped.(backup.TreeFetcher)
	if !ok {
		return nil, backup.ErrNotImplemented
	}

	objects, err := fetcher.GetTree(ctx, ref, maxDepth)
	if err != nil {
		return nil, err
	}

	for _, obj := range objects {
		if s.test(obj) {
			_ = s.cache.Put(ctx, obj)
		}
	}

	return objects, nil
}

// Get implements backup.ObjectStore.
func (s *Store) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	obj, _ := s.cache.Get(ctx, ref)

	if obj != nil {
		return obj, nil
	}

	return s.wrapped.Get(ctx, ref)
}

// Delete implements backup.ObjectStore.
func (s *Store) Delete(ctx context.Context, ref *proto.Ref) error {
	err := s.wrapped.Delete(ctx, ref)
	if err == nil {
		_ = s.cache.Delete(ctx, ref)
	}

	return err
}

// Walk implements backup.ObjectStore.
func (s *Store) Walk(ctx context.Context, b bool, objectType proto.ObjectType, receiver backup.ObjectReceiver) error {
	return s.wrapped.Walk(ctx, b, objectType, receiver)
}

// Has implements backup.ObjectStore.
func (s *Store) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	has, err := s.cache.Has(ctx, ref)
	if err == nil && has {
		return has, err
	}

	return s.wrapped.Has(ctx, ref)
}
