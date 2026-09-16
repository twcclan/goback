package backup

import (
	"context"
	"path"
	"sync"

	"github.com/twcclan/goback/proto"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type concurrentTreeNode struct {
	prefix string
	object *proto.Object
}

var SkipTree = errors.New("skip tree")

type TraverserHandler func(string, *proto.TreeNode) error

func TraverseTree(ctx context.Context, store ObjectStore, tree *proto.Object, workers int, handler TraverserHandler) error {
	tr := &concurrentTreeTraverser{
		store:      store,
		queue:      make(chan *concurrentTreeNode),
		traverseFn: handler,
	}

	return tr.run(ctx, tree, workers)
}

type concurrentTreeTraverser struct {
	store      ObjectStore
	queue      chan *concurrentTreeNode
	wg         sync.WaitGroup
	traverseFn TraverserHandler
}

func (c *concurrentTreeTraverser) run(ctx context.Context, root *proto.Object, workers int) error {
	grp, ctx := errgroup.WithContext(ctx)

	if workers < 1 {
		return errors.New("Need at least one worker")
	}

	for i := 0; i < workers; i++ {
		grp.Go(c.traverser(ctx))
	}

	c.wg.Add(1)
	c.queue <- &concurrentTreeNode{
		object: root,
	}

	c.wg.Wait()

	close(c.queue)

	return grp.Wait()
}

func (c *concurrentTreeTraverser) traverseTree(ctx context.Context, t *concurrentTreeNode) error {
	// we traverse depth-first to distribute the work to as many goroutines
	// as possible. this means we're going to iterate twice, because our
	// trees nodes are sorted lexicographically
	for _, node := range t.object.GetTree().GetNodes() {
		info := node.Stat
		if !info.IsDir() {
			continue
		}

		err := c.traverseFn(path.Join(t.prefix, info.Name), node)
		if err != nil {
			// if the TraverseFunc signals that we should skip the tree
			// we will just continue and won't create a new concurrentTreeNode
			if errors.Is(err, SkipTree) {
				continue
			}

			return err
		}

		// retrieve the sub-tree object, flattening any splits
		subTree, err := LoadTree(ctx, c.store, node.Ref)
		if err != nil {
			return errors.Wrapf(err, "Sub tree %x could not be retrieved", node.Ref.Hash)
		}

		subTreeNode := &concurrentTreeNode{
			prefix: path.Join(t.prefix, info.Name),
			object: proto.NewObject(subTree),
		}

		c.wg.Add(1)
		// try to hand to an idle worker
		select {
		case c.queue <- subTreeNode:

		// if no other worker is idle, do the job ourselves
		default:
			c.wg.Done()
			err = c.traverseTree(ctx, subTreeNode)
			if err != nil {
				return err
			}
		}
	}

	// iterate a second time for the files
	for _, node := range t.object.GetTree().GetNodes() {
		info := node.Stat
		if info.IsDir() {
			continue
		}

		err := c.traverseFn(path.Join(t.prefix, info.Name), node)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *concurrentTreeTraverser) traverser(ctx context.Context) func() error {
	return func() error {
		for {
			done := ctx.Done()

			select {
			case n := <-c.queue:
				if n == nil {
					return nil
				}

				err := c.traverseTree(ctx, n)
				c.wg.Done()

				if err != nil {
					return err
				}

			case <-done:
				return ctx.Err()
			}
		}
	}
}
