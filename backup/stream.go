package backup

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/twcclan/goback/proto"
)

// Stream is a file a backup reads from a stream, such as a command's
// output, instead of from the disk.
type Stream struct {
	// Name is the file's name in the commit's tree.
	Name    string
	Content io.Reader
	// Finish, when set, runs once Content is read to its end and before
	// the commit is stored; an error fails the run without a commit.
	Finish func() error
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)

	return n, err
}

// putStream stores the stream as the run's one file.
func (w *Walker) putStream(ctx context.Context) (*proto.TreeNode, error) {
	content := &countingReader{r: w.Stream.Content}

	ref, err := PutFile(ctx, w.Objects, w.Key, -1, content)
	if err != nil {
		return nil, fmt.Errorf("storing %s: %w", w.Stream.Name, err)
	}

	if w.Stream.Finish != nil {
		if err := w.Stream.Finish(); err != nil {
			return nil, err
		}
	}

	atomic.AddInt64(&w.result.Files, 1)
	atomic.AddInt64(&w.result.Read, 1)
	atomic.AddInt64(&w.result.Bytes, content.n)

	return &proto.TreeNode{
		Stat: &proto.FileInfo{
			Name:    []byte(w.Stream.Name),
			Type:    proto.NodeType_NODE_FILE,
			Size:    content.n,
			Mode:    0o600,
			MtimeNs: w.now().UnixNano(),
		},
		Ref: ref,
	}, nil
}
