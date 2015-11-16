package storage

import (
	"fmt"
	"path/filepath"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/s3"
)

func NewS3ChunkStore(auth aws.Auth, region aws.Region, bucket string) *S3ChunkStore {
	s3 := s3.New(auth, region)
	return &S3ChunkStore{bucket: s3.Bucket(bucket)}
}

var _ backup.ChunkStore = (*S3ChunkStore)(nil)

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

func (s *S3ChunkStore) Walk(chunkType proto.ChunkType, fn backup.ChunkWalkFn) error {
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
