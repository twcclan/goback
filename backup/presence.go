package backup

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/twcclan/goback/backup/presence"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"

	"golang.org/x/sync/errgroup"
)

// PresenceScope says whose commits feed the filters an agent receives.
type PresenceScope int

const (
	// PresenceOff hands out no filters.
	PresenceOff PresenceScope = iota
	// PresenceSet covers the agent's own set.
	PresenceSet
	// PresenceStore covers every set of the store.
	PresenceStore
)

// ParsePresenceScope reads off, set or store.
func ParsePresenceScope(s string) (PresenceScope, error) {
	switch strings.ToLower(s) {
	case "off":
		return PresenceOff, nil
	case "set":
		return PresenceSet, nil
	case "store":
		return PresenceStore, nil
	}

	return PresenceOff, fmt.Errorf("unknown presence scope %q", s)
}

// String implements fmt.Stringer.
func (s PresenceScope) String() string {
	switch s {
	case PresenceSet:
		return "set"
	case PresenceStore:
		return "store"
	}

	return "off"
}

// PresenceSource hands an agent the filters of its scope.
type PresenceSource interface {
	// Presence returns the filters for a run on set.
	Presence(ctx context.Context, set string) (presence.Set, error)
}

// Confirmer stores a file object once the store holds every part the
// caller assumed; otherwise it stores nothing and names the missing refs.
type Confirmer interface {
	// PutFile stores obj when every assumed ref is held; otherwise it
	// returns the refs that are not.
	PutFile(ctx context.Context, obj *proto.Object, assumed []*proto.Ref) (missing []*proto.Ref, err error)
}

// PresenceIndex serves the filters of a scope.
type PresenceIndex interface {
	// Presence returns the filters of scope; set matters for PresenceSet.
	Presence(ctx context.Context, scope PresenceScope, set string) ([]*proto.PresenceFilter, error)
}

// PolicySource knows the store's policy; nil means the store has none set.
type PolicySource interface {
	// StorePolicy returns the store's policy, or nil.
	StorePolicy(ctx context.Context) (*storekey.Policy, error)
}

// Missing returns the refs the store does not hold, in the order given.
func Missing(ctx context.Context, store ObjectStore, refs []*proto.Ref) ([]*proto.Ref, error) {
	present := make([]bool, len(refs))
	grp, ctx := errgroup.WithContext(ctx)
	grp.SetLimit(16)

	for i, ref := range refs {
		grp.Go(func() error {
			has, err := store.Has(ctx, ref)
			present[i] = has

			return err
		})
	}

	if err := grp.Wait(); err != nil {
		return nil, err
	}

	var missing []*proto.Ref
	for i, ref := range refs {
		if !present[i] {
			missing = append(missing, ref)
		}
	}

	return missing, nil
}

// CollectPresence walks a tree and returns a filter over every blob ref it
// reaches.
func CollectPresence(ctx context.Context, store ObjectStore, tree *proto.Ref) (*presence.Filter, error) {
	root, err := LoadTree(ctx, store, tree)
	if err != nil {
		return nil, err
	}

	var (
		mtx  sync.Mutex
		refs = map[string]struct{}{}
	)

	collect := func(parts []*proto.FilePart) {
		mtx.Lock()
		for _, part := range parts {
			refs[string(part.GetRef().GetHash())] = struct{}{}
		}
		mtx.Unlock()
	}

	err = TraverseTree(ctx, store, proto.NewObject(root), 32, func(_ string, node *proto.TreeNode) error {
		if node.GetStat().GetType() != proto.NodeType_NODE_FILE || node.Ref == nil {
			return nil
		}

		obj, err := store.Get(ctx, node.Ref)
		if err != nil {
			return fmt.Errorf("file %x: %w", node.Ref.Hash, err)
		}

		file := obj.GetFile()
		if file == nil {
			return fmt.Errorf("object %x is not a file", node.Ref.Hash)
		}

		parts, err := FileParts(ctx, store, file)
		if err != nil {
			return fmt.Errorf("file %x: %w", node.Ref.Hash, err)
		}

		collect(parts)

		return nil
	})
	if err != nil {
		return nil, err
	}

	filter := presence.New(uint64(len(refs)))
	for ref := range refs {
		filter.Add([]byte(ref))
	}

	return filter, nil
}
