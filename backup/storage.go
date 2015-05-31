package backup

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/twcclan/goback/proto"

	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/s3"
	_ "github.com/joho/godotenv/autoload"
)

type ChunkWalkFn func(*proto.ChunkRef) error

type ChunkStore interface {
	Create(*proto.Chunk) error
	Read(*proto.ChunkRef) (*proto.Chunk, error)
	Delete(*proto.ChunkRef) error
	Walk(proto.ChunkType, ChunkWalkFn) error
}

func NewSimpleChunkStore(base string) *SimpleChunkStore {
	return &SimpleChunkStore{
		base: base,
	}
}

var _ ChunkStore = (*SimpleChunkStore)(nil)

type SimpleChunkStore struct {
	base string
}

func (s *SimpleChunkStore) chunkFilename(ref *proto.ChunkRef) string {
	//chunk := fmt.Sprintf("%x/%x/%x", ref.Sum[:2], ref.Sum[2:4], ref.Sum)
	chunk := fmt.Sprintf("%d-%x", ref.Type, ref.Sum)

	return path.Join(s.base, chunk)
}

func (s *SimpleChunkStore) Create(chunk *proto.Chunk) error {
	return ioutil.WriteFile(s.chunkFilename(chunk.Ref), chunk.Data, 0644)
}

func (s *SimpleChunkStore) Delete(ref *proto.ChunkRef) error {
	return os.Remove(s.chunkFilename(ref))
}

func (s *SimpleChunkStore) Read(ref *proto.ChunkRef) (*proto.Chunk, error) {
	data, err := ioutil.ReadFile(s.chunkFilename(ref))

	return &proto.Chunk{
		Ref:  ref,
		Data: data,
	}, err
}

func (s *SimpleChunkStore) Walk(chunkType proto.ChunkType, fn ChunkWalkFn) error {
	matches, err := filepath.Glob(path.Join(s.base, fmt.Sprintf("%d-*", chunkType)))
	if err != nil {
		return err
	}

	for _, match := range matches {
		var hexSum []byte

		fmt.Sscanf(filepath.Base(match)[2:], "%x", &hexSum)

		err = fn(&proto.ChunkRef{Sum: hexSum, Type: chunkType})
		if err != nil {
			return err
		}
	}

	return nil
}

func NewS3ChunkStore(auth aws.Auth, region aws.Region, bucket string) *S3ChunkStore {
	s3 := s3.New(auth, region)
	return &S3ChunkStore{bucket: s3.Bucket(bucket)}
}

var _ ChunkStore = (*S3ChunkStore)(nil)

type S3ChunkStore struct {
	bucket *s3.Bucket
}

func (s *S3ChunkStore) chunkKey(ref *proto.ChunkRef) string {
	return fmt.Sprintf("%d-%x", ref.Type, ref.Sum)
}

func (s *S3ChunkStore) Create(chunk *proto.Chunk) error {
	return s.bucket.Put(s.chunkKey(chunk.Ref), chunk.Data, "application/octet-stream", s3.Private, s3.Options{})
}

func (s *S3ChunkStore) Delete(ref *proto.ChunkRef) error {
	return s.bucket.Del(s.chunkKey(ref))
}

func (s *S3ChunkStore) Read(ref *proto.ChunkRef) (*proto.Chunk, error) {
	data, err := s.bucket.Get(s.chunkKey(ref))
	if err != nil {
		return nil, err
	}

	return &proto.Chunk{
		Ref:  ref,
		Data: data,
	}, nil
}

func (s *S3ChunkStore) Walk(chunkType proto.ChunkType, fn ChunkWalkFn) error {
	resp, err := s.bucket.List(fmt.Sprintf("%d", chunkType), "", "", 1000)
	if err != nil {
		return err
	}

	for _, key := range resp.Contents {
		var hexSum []byte

		fmt.Sscanf(filepath.Base(key.Key)[2:], "%x", &hexSum)

		err = fn(&proto.ChunkRef{Sum: hexSum, Type: chunkType})
		if err != nil {
			return err
		}
	}

	return nil
}
