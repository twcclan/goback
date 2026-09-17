package proto

//go:generate protoc --go_out=paths=source_relative:. --go-grpc_out=paths=source_relative:. api.proto blob.proto commit.proto file.proto index.proto object.proto pin.proto presence.proto ref.proto tree.proto transactional.proto

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
	pb "google.golang.org/protobuf/proto"
)

// Bytes marshals a message and panics on the (unreachable) marshal errors.
func Bytes(m pb.Message) []byte {
	data, err := pb.Marshal(m)
	if err != nil {
		panic(err)
	}

	return data
}

// Compress gzips a payload.
func Compress(payload []byte) []byte {
	buf := new(bytes.Buffer)
	writer := gzip.NewWriter(buf)

	_, err := writer.Write(payload)
	if err != nil {
		panic(err)
	}

	err = writer.Close()
	if err != nil {
		panic(err)
	}

	return buf.Bytes()
}

// CompressedBytes marshals and gzips a message.
func CompressedBytes(m pb.Message) []byte {
	return Compress(Bytes(m))
}

func decompressedBytes(compressed []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}

	b, err := io.ReadAll(reader)
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

func Size(msg pb.Message) int {
	return pb.Size(msg)
}

func EncodeVarint(buf []byte, x uint64) []byte {
	return protowire.AppendVarint(buf, x)
}

func DecodeVarint(buf []byte) (x uint64, n int) {
	return protowire.ConsumeVarint(buf)
}

func (o *Object) Type() ObjectType {
	switch t := o.GetObject().(type) {
	case *Object_Commit:
		return ObjectType_COMMIT
	case *Object_Tree:
		return ObjectType_TREE
	case *Object_Blob:
		return ObjectType_BLOB
	case *Object_File:
		return ObjectType_FILE
	case *Object_Sealed:
		return t.Sealed.GetType()
	case *Object_Pin:
		return ObjectType_PIN
	default:
		return ObjectType_INVALID
	}
}

// Bytes marshals the Object wrapper; it is a transport encoding, not the
// hashed payload (see Canonical).
func (o *Object) Bytes() []byte {
	return Bytes(o)
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
	case *Sealed:
		out = &Object_Sealed{t}
	case *Pin:
		out = &Object_Pin{t}
	default:
		panic("Unsupported object type")
	}

	return &Object{Object: out}
}

func NewObjectHeaderFromBytes(bytes []byte) (*ObjectHeader, error) {
	hdr := new(ObjectHeader)

	return hdr, pb.Unmarshal(bytes, hdr)
}

// NewObjectFromBytes decodes an Object wrapper written by Object.Bytes.
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

// GetFileInfo converts stat data into a FileInfo. Owner names and the
// symlink target come from a FileInfo returned by Sys(), as WithDetails
// and GetOSFileInfo arrange.
func GetFileInfo(info os.FileInfo) *FileInfo {
	fi := &FileInfo{
		Name:    []byte(info.Name()),
		Mode:    uint32(info.Mode()),
		MtimeNs: info.ModTime().UnixNano(),
		Size:    info.Size(),
		Type:    NodeTypeOf(info.Mode()),
	}

	if details, ok := info.Sys().(*FileInfo); ok {
		fi.User = details.User
		fi.Group = details.Group
		fi.LinkTarget = details.LinkTarget
	}

	return fi
}

// WithDetails attaches owner names and a symlink target to stat data so
// GetFileInfo records them.
func WithDetails(info os.FileInfo, user, group, linkTarget string) os.FileInfo {
	return &detailedFileInfo{
		FileInfo: info,
		details:  &FileInfo{User: []byte(user), Group: []byte(group), LinkTarget: []byte(linkTarget)},
	}
}

type detailedFileInfo struct {
	os.FileInfo
	details *FileInfo
}

func (d *detailedFileInfo) Sys() interface{} {
	return d.details
}

// NodeTypeOf maps a file mode to the node type recorded in a tree.
func NodeTypeOf(mode os.FileMode) NodeType {
	switch {
	case mode.IsDir():
		return NodeType_NODE_DIRECTORY
	case mode&os.ModeSymlink != 0:
		return NodeType_NODE_SYMLINK
	default:
		return NodeType_NODE_FILE
	}
}

// IsDir reports whether the node describes a directory.
func (fi *FileInfo) IsDir() bool {
	return fi.GetType() == NodeType_NODE_DIRECTORY
}

// ModTime returns the recorded modification time.
func (fi *FileInfo) ModTime() time.Time {
	return time.Unix(0, fi.GetMtimeNs())
}

type backupFileInfo struct {
	*FileInfo
}

func (bi *backupFileInfo) IsDir() bool {
	return bi.FileInfo.IsDir()
}

func (bi *backupFileInfo) Name() string {
	return string(bi.FileInfo.Name)
}

func (bi *backupFileInfo) ModTime() time.Time {
	return bi.FileInfo.ModTime()
}

func (bi *backupFileInfo) Mode() os.FileMode {
	return os.FileMode(bi.FileInfo.Mode)
}

func (bi *backupFileInfo) Size() int64 {
	return bi.FileInfo.Size
}

// Sys returns the recorded FileInfo, so restore can read the symlink target
// and owner names.
func (bi *backupFileInfo) Sys() interface{} {
	return bi.FileInfo
}

func GetOSFileInfo(info *FileInfo) os.FileInfo {
	return &backupFileInfo{info}
}
