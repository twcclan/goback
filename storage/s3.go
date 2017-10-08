package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

const s3Key = "pack/%s/%s" // pack/<extension>/<filename>

type S3 struct {
	bucket string
	client *s3.S3
}

func (s *S3) key(ref *proto.Ref) string {
	return fmt.Sprintf("pack/%x/%x/%x", ref.Sha1[:2], ref.Sha1[2:4], ref.Sha1)
}

func (s *S3) Delete(ref *proto.Ref) error {
	_, err := s.client.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(ref)),
	})

	return err
}

func (s *S3) Get(ref *proto.Ref) (*proto.Object, error) {
	resp, err := s.client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(ref)),
	})

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return proto.NewObjectFromCompressedBytes(bytes)
}

func (s *S3) Put(obj *proto.Object) error {
	data := obj.CompressedBytes()

	_, err := s.client.PutObject(&s3.PutObjectInput{
		Body:   bytes.NewReader(data),
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(obj.Ref())),
	})

	return err
}

func (s *S3) Walk(load bool, objType proto.ObjectType, fn backup.ObjectReceiver) error {
	return errors.New("Not implemented")
}

var _ backup.ObjectStore = (*S3)(nil)

func NewS3() backup.ObjectStore {
	session := session.New()
	client := s3.New(session, aws.NewConfig().
		WithRegion("nyc3").
		WithEndpoint("https://nyc3.digitaloceanspaces.com").
		WithCredentials(credentials.NewStaticCredentials("WYOJUDTPOAJTPFSBHUDO", "2Z+Sczmh8DAfo9zCdZOpNoe7c4JB2Z9/ch+EOC7LDu8", "")))

	return &S3{
		bucket: "goback-test",
		client: client,
	}
}

// WYOJUDTPOAJTPFSBHUDO
// 2Z+Sczmh8DAfo9zCdZOpNoe7c4JB2Z9/ch+EOC7LDu8
