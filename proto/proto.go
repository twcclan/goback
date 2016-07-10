package proto

//go:generate protoc --go_out=plugins=grpc:. *.proto

import (
	"crypto/sha1"
	"os"
	"time"

	pb "github.com/golang/protobuf/proto"
)

func protoBytes(m pb.Message) []byte {
	if data, err := pb.Marshal(m); err != nil {
		// there are only a few very specific error conditions
		// that shouldn't ever affect us
		panic(err)
	} else {
		return data
	}
	//return []byte(pb.MarshalTextString(o))
}

func (i *Index) Bytes() []byte {
	return protoBytes(i)
}

func Size(msg pb.Message) int {
	return pb.Size(msg)
}

func (o *Object) Type() ObjectType {
	switch o.GetObject().(type) {
	case *Object_Commit:
		return ObjectType_COMMIT
	case *Object_Tree:
		return ObjectType_TREE
	case *Object_Blob:
		return ObjectType_BLOB
	case *Object_File:
		return ObjectType_FILE
	default:
		return ObjectType_INVALID
	}
}

func (o *Object) Ref() *Ref {
	sum := sha1.Sum(o.Bytes())
	return &Ref{Sha1: sum[:]}
}

func (o *Object) Bytes() []byte {
	return protoBytes(o)
}

func NewObject(in interface{}) *Object {
	var out isObject_Object

	switch t := in.(type) {
	case *Commit:
		out = &Object_Commit{t}
	case *Tree:
		out = &Object_Tree{t}
	case *Blob:
		out = &Object_Blob{t}
	case *File:
		out = &Object_File{t}
	default:
		panic("Unsupported object type")
	}

	return &Object{out}
}

func NewObjectFromBytes(bytes []byte) (*Object, error) {
	obj := new(Object)

	return obj, pb.Unmarshal(bytes, obj)
}

func GetFileInfo(info os.FileInfo) *FileInfo {
	return &FileInfo{
		Name:      info.Name(),
		Mode:      uint32(info.Mode()),
		Timestamp: info.ModTime().UTC().Unix(),
		Size:      info.Size(),
		Tree:      info.IsDir(),
	}
}

type backupFileInfo struct {
	*FileInfo
}

func (bi *backupFileInfo) IsDir() bool {
	return false
}

func (bi *backupFileInfo) Name() string {
	return bi.FileInfo.Name
}

func (bi *backupFileInfo) ModTime() time.Time {
	return time.Unix(bi.Timestamp, 0)
}

func (bi *backupFileInfo) Mode() os.FileMode {
	return os.FileMode(bi.FileInfo.Mode)
}

func (bi *backupFileInfo) Size() int64 {
	return bi.FileInfo.Size
}

func (bi *backupFileInfo) Sys() interface{} {
	return nil
}

func GetOSFileInfo(info *FileInfo) os.FileInfo {
	return &backupFileInfo{info}
}

/*
func GetMetaChunk(msg proto.Message, chunkType ChunkType) *Chunk {
	data, err := proto.Marshal(msg)
	if err != nil {
		panic(err)
	}

	sum := sha1.Sum(data)

	chunk := &Chunk{
		Ref: &ChunkRef{
			Sum:  sum[:],
			Type: chunkType,
		},
		Data: data,
	}

	return chunk
}

func ReadMetaChunk(chunk *Chunk, msg proto.Message) {
	err := proto.Unmarshal(chunk.Data, msg)
	if err != nil {
		panic(err)
	}
}

func GetTypedRef(ref *ChunkRef, chunkType ChunkType) *ChunkRef {
	return &ChunkRef{
		Sum:  ref.Sum,
		Type: chunkType,
	}
}

func GetUntypedRef(ref *ChunkRef) *ChunkRef {
	return &ChunkRef{
		Sum: ref.Sum,
	}
}
*/
