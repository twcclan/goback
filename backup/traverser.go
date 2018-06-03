package backup

import (
	"context"
	"path"
	"sync"

	"github.com/pkg/errors"
	"github.com/twcclan/goback/proto"
	"golang.org/x/sync/errgroup"
)

type concurrentTreeNode struct {
	prefix string
	object *proto.Object
}

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
		if !info.Tree {
			continue
		}

		err := c.traverseFn(path.Join(t.prefix, info.Name), node)
		if err != nil {
			return err
		}

		// retrieve the sub-tree object
		subTree, err := c.store.Get(ctx, node.Ref)

		if err != nil {
			return err
		}

		if subTree == nil {
			return errors.Errorf("Sub tree %x could not be retrieved", node.Ref.Sha1)
		}

		subTreeNode := &concurrentTreeNode{
			prefix: path.Join(t.prefix, info.Name),
			object: subTree,
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
		if info.Tree {
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
