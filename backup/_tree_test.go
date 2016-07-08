package backup

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/twcclan/goback/proto"
)

type digraphChunkStore struct {
}

func (dg *digraphChunkStore) vertex(source, target *proto.Ref) {
	dg.vertexLabel(source, target, "")
}

func (dg *digraphChunkStore) vertexLabel(source, target *proto.Ref, label string, params ...interface{}) {
	fmt.Printf("\"%x\" -> \"%x\" [label=\"%s\"];\n", source.Sha1, target.Sha1, fmt.Sprintf(label, params...))
}

func (dg *digraphChunkStore) label(node *proto.Ref, label string, params ...interface{}) {
	fmt.Printf("\"%x\" [label=\"%s\"];\n", node.Sha1, fmt.Sprintf(label, params...))
}

func (dg *digraphChunkStore) Put(obj *proto.Object) error {
	switch obj.Type() {
	case proto.ObjectType_TREE:
		for _, target := range obj.GetTree().GetNodes() {
			label := target.GetStat().Name
			if target.GetStat().Tree {
				label += "/"
				dg.label(target.GetRef(), "Tree")
			} else {
				dg.label(target.GetRef(), "File")
			}

			dg.vertexLabel(obj.Ref(), target.GetRef(), label)
		}
	}

	if commit := obj.GetCommit(); commit != nil {
		dg.vertex(obj.Ref(), commit.GetTree())
		dg.label(obj.Ref(), "Commit %s", time.Unix(commit.Timestamp, 0))
		dg.label(commit.GetTree(), "Root")
	}

	for _, part := range obj.GetFile().GetParts() {
		dg.vertexLabel(obj.Ref(), part.Ref, "%d - %d", part.Offset, part.Offset+part.Length)
	}

	if blob := obj.GetBlob(); blob != nil {
		dg.label(obj.Ref(), "Blob: %d bytes", len(blob.Data))
	}

	return nil
}

func (dg *digraphChunkStore) Get(ref *proto.Ref) (*proto.Object, error) {
	return nil, nil
}

func (dg *digraphChunkStore) Delete(ref *proto.Ref) error {
	return nil
}

func (dg *digraphChunkStore) Walk(t proto.ObjectType, f ChunkWalkFn) error {
	return nil
}

type filesystemNode struct {
	nodes []filesystemNode
	info  os.FileInfo
}

const testFiles = "./testfiles"

func TestTree(t *testing.T) {
	c := &commitWriter{
		store: &digraphChunkStore{},
	}

	cm, err := c.do(testFiles)
	if err != nil {
		log.Fatal(err)
	}

	log.Println(cm.String())
}
