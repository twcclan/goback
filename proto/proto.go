package proto

//go:generate protoc --go_out=plugins=grpc:. *.proto

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"io/ioutil"
	"os"
	"sort"
	"time"

	pb "github.com/golang/protobuf/proto"
)

var testMap = map[pb.Message][]byte{}

func Bytes(m pb.Message) []byte {
	if data, err := pb.Marshal(m); err != nil {
		// there are only a few very specific error conditions
		// that shouldn't ever affect us
		panic(err)
	} else {
		return data
	}
	//return []byte(pb.MarshalTextString(o))
}

func CompressedBytes(m pb.Message) []byte {
	buf := new(bytes.Buffer)
	writer := gzip.NewWriter(buf)

	_, err := writer.Write(Bytes(m))
	if err != nil {
		panic(err)
	}

	err = writer.Close()
	if err != nil {
		panic(err)
	}

	return buf.Bytes()
}

func decompressedBytes(compressed []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}

	b, err := ioutil.ReadAll(reader)

	if err != nil {
		return nil, err
	}

	return b, reader.Close()
}

func (i *Index) Bytes() []byte {
	return Bytes(i)
}

func (i *Index) CompressedBytes() []byte {
	return CompressedBytes(i)
}

func (i *Index) Lookup(ref *Ref) *Location {
	locs := i.Locations

	n := sort.Search(len(locs), func(i int) bool {
		return bytes.Compare(locs[i].Ref.Sha1, ref.Sha1) >= 0
	})

	if n < len(locs) && bytes.Equal(locs[n].Ref.Sha1, ref.Sha1) {
		return locs[n]
	}

	return nil
}

func Size(msg pb.Message) int {
	return pb.Size(msg)
}

func EncodeVarint(x uint64) []byte {
	return pb.EncodeVarint(x)
}

func DecodeVarint(buf []byte) (x uint64, n int) {
	return pb.DecodeVarint(buf)
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
	return Bytes(o)
}

func (o *Object) CompressedBytes() []byte {
	return CompressedBytes(o)
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

func NewObjectHeaderFromBytes(bytes []byte) (*ObjectHeader, error) {
	hdr := new(ObjectHeader)

	return hdr, pb.Unmarshal(bytes, hdr)
}

func NewObjectFromCompressedBytes(bytes []byte) (*Object, error) {
	b, err := decompressedBytes(bytes)

	if err != nil {
		return nil, err
	}

	return NewObjectFromBytes(b)
}

func NewObjectFromBytes(bytes []byte) (*Object, error) {
	obj := new(Object)

	return obj, pb.Unmarshal(bytes, obj)
}

func NewIndexFromCompressedBytes(bytes []byte) (*Index, error) {
	b, err := decompressedBytes(bytes)

	if err != nil {
		return nil, err
	}

	return NewIndexFromBytes(b)
}

func NewIndexFromBytes(bytes []byte) (*Index, error) {
	idx := new(Index)

	return idx, pb.Unmarshal(bytes, idx)
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
	return bi.Tree
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
