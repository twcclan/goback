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

var _ backup.ObjectStore = (*S3ChunkStore)(nil)

type S3ChunkStore struct {
	bucket *s3.Bucket
}

func (s *S3ChunkStore) chunkKey(ref *proto.Ref) string {
	return fmt.Sprintf("%x", ref.Sha1)
}

func (s *S3ChunkStore) Put(chunk *proto.Object) error {
	return s.bucket.Put(s.chunkKey(chunk.Ref()), chunk.Bytes(), "application/octet-stream", s3.Private, s3.Options{})
}

func (s *S3ChunkStore) Delete(ref *proto.Ref) error {
	return s.bucket.Del(s.chunkKey(ref))
}

func (s *S3ChunkStore) Get(ref *proto.Ref) (*proto.Object, error) {
	data, err := s.bucket.Get(s.chunkKey(ref))
	if err != nil {
		return nil, err
	}

	return proto.NewObjectFromBytes(data)
}

func (s *S3ChunkStore) Walk(chunkType proto.ObjectType, fn backup.ChunkWalkFn) error {
	resp, err := s.bucket.List(fmt.Sprintf("%d", chunkType), "", "", 1000)
	if err != nil {
		return err
	}

	for _, key := range resp.Contents {
		var hexSum []byte

		fmt.Sscanf(filepath.Base(key.Key), "%x", &hexSum)

		err = fn(&proto.Ref{Sha1: hexSum})
		if err != nil {
			return err
		}
	}

	return nil
}
