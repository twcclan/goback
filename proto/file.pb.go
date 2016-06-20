// Code generated by protoc-gen-go.
// source: file.proto
// DO NOT EDIT!

package proto

import proto1 "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto1.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// this contains file metadata, which can be different
// accross backups, even if file content is the same
type FileInfo struct {
	Name      string `protobuf:"bytes,1,opt,name=name" json:"name,omitempty"`
	Mode      uint32 `protobuf:"varint,2,opt,name=mode" json:"mode,omitempty"`
	User      string `protobuf:"bytes,3,opt,name=user" json:"user,omitempty"`
	Group     string `protobuf:"bytes,4,opt,name=group" json:"group,omitempty"`
	Timestamp int64  `protobuf:"varint,5,opt,name=timestamp" json:"timestamp,omitempty"`
	Size      int64  `protobuf:"varint,6,opt,name=size" json:"size,omitempty"`
	Tree      bool   `protobuf:"varint,7,opt,name=tree" json:"tree,omitempty"`
}

func (m *FileInfo) Reset()                    { *m = FileInfo{} }
func (m *FileInfo) String() string            { return proto1.CompactTextString(m) }
func (*FileInfo) ProtoMessage()               {}
func (*FileInfo) Descriptor() ([]byte, []int) { return fileDescriptor3, []int{0} }

type File struct {
	Parts []*FilePart `protobuf:"bytes,1,rep,name=parts" json:"parts,omitempty"`
}

func (m *File) Reset()                    { *m = File{} }
func (m *File) String() string            { return proto1.CompactTextString(m) }
func (*File) ProtoMessage()               {}
func (*File) Descriptor() ([]byte, []int) { return fileDescriptor3, []int{1} }

func (m *File) GetParts() []*FilePart {
	if m != nil {
		return m.Parts
	}
	return nil
}

type FilePart struct {
	Offset uint64 `protobuf:"varint,1,opt,name=offset" json:"offset,omitempty"`
	Length uint64 `protobuf:"varint,2,opt,name=length" json:"length,omitempty"`
	Ref    *Ref   `protobuf:"bytes,3,opt,name=ref" json:"ref,omitempty"`
}

func (m *FilePart) Reset()                    { *m = FilePart{} }
func (m *FilePart) String() string            { return proto1.CompactTextString(m) }
func (*FilePart) ProtoMessage()               {}
func (*FilePart) Descriptor() ([]byte, []int) { return fileDescriptor3, []int{2} }

func (m *FilePart) GetRef() *Ref {
	if m != nil {
		return m.Ref
	}
	return nil
}

func init() {
	proto1.RegisterType((*FileInfo)(nil), "proto.FileInfo")
	proto1.RegisterType((*File)(nil), "proto.File")
	proto1.RegisterType((*FilePart)(nil), "proto.FilePart")
}

var fileDescriptor3 = []byte{
	// 239 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0x44, 0x90, 0x4f, 0x4b, 0x03, 0x31,
	0x10, 0xc5, 0x59, 0xf7, 0x8f, 0xdd, 0x29, 0x22, 0x04, 0x91, 0x20, 0x1e, 0x64, 0x41, 0xe8, 0xc5,
	0x1e, 0xea, 0x77, 0x10, 0xbc, 0xc9, 0x9c, 0xbc, 0xae, 0x38, 0xa9, 0x0b, 0xdd, 0x4d, 0x48, 0xd2,
	0x8b, 0x1f, 0xc7, 0x4f, 0xea, 0xcc, 0x64, 0xa1, 0xa7, 0xbc, 0xf7, 0x7b, 0x43, 0x32, 0x2f, 0x00,
	0x6e, 0x3a, 0xd1, 0x3e, 0x44, 0x9f, 0xbd, 0x69, 0xf5, 0x78, 0xe8, 0x23, 0xb9, 0x42, 0x86, 0xbf,
	0x0a, 0x36, 0x6f, 0x3c, 0xf0, 0xbe, 0x38, 0x6f, 0x0c, 0x34, 0xcb, 0x38, 0x93, 0xad, 0x9e, 0xaa,
	0x5d, 0x8f, 0xaa, 0x85, 0xcd, 0xfe, 0x9b, 0xec, 0x15, 0xb3, 0x1b, 0x54, 0x2d, 0xec, 0x9c, 0x28,
	0xda, 0xba, 0xcc, 0x89, 0x36, 0x77, 0xd0, 0x1e, 0xa3, 0x3f, 0x07, 0xdb, 0x28, 0x2c, 0xc6, 0x3c,
	0x42, 0x9f, 0xa7, 0x99, 0x52, 0x1e, 0xe7, 0x60, 0x5b, 0x4e, 0x6a, 0xbc, 0x00, 0xb9, 0x27, 0x4d,
	0xbf, 0x64, 0x3b, 0x0d, 0x54, 0x0b, 0xcb, 0x91, 0xc8, 0x5e, 0x33, 0xdb, 0xa0, 0xea, 0xe1, 0x05,
	0x1a, 0xd9, 0xd1, 0x3c, 0x43, 0x1b, 0xc6, 0x98, 0x13, 0x2f, 0x58, 0xef, 0xb6, 0x87, 0xdb, 0xd2,
	0x61, 0x2f, 0xd9, 0x07, 0x73, 0x2c, 0xe9, 0xf0, 0x59, 0x2a, 0x09, 0x32, 0xf7, 0xd0, 0x79, 0xe7,
	0x12, 0x65, 0x2d, 0xd5, 0xe0, 0xea, 0x84, 0x9f, 0x68, 0x39, 0xe6, 0x1f, 0x2d, 0xc6, 0xbc, 0x38,
	0x5e, 0xb8, 0xe6, 0xcf, 0xd1, 0x66, 0xdb, 0x03, 0xac, 0x0f, 0x20, 0x39, 0x14, 0xfc, 0xd5, 0xa9,
	0x7f, 0xfd, 0x0f, 0x00, 0x00, 0xff, 0xff, 0x7c, 0xf4, 0xc2, 0x51, 0x54, 0x01, 0x00, 0x00,
}
