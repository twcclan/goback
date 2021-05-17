// Code generated by protoc-gen-go. DO NOT EDIT.
// source: file.proto

package proto

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

// this contains file metadata, which can be different
// accross backups, even if file content is the same
type FileInfo struct {
	Name                 string   `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Mode                 uint32   `protobuf:"varint,2,opt,name=mode,proto3" json:"mode,omitempty"`
	User                 string   `protobuf:"bytes,3,opt,name=user,proto3" json:"user,omitempty"`
	Group                string   `protobuf:"bytes,4,opt,name=group,proto3" json:"group,omitempty"`
	Timestamp            int64    `protobuf:"varint,5,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
	Size                 int64    `protobuf:"varint,6,opt,name=size,proto3" json:"size,omitempty"`
	Tree                 bool     `protobuf:"varint,7,opt,name=tree,proto3" json:"tree,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *FileInfo) Reset()         { *m = FileInfo{} }
func (m *FileInfo) String() string { return proto.CompactTextString(m) }
func (*FileInfo) ProtoMessage()    {}
func (*FileInfo) Descriptor() ([]byte, []int) {
	return fileDescriptor_file_1ca139b73b0b924b, []int{0}
}
func (m *FileInfo) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_FileInfo.Unmarshal(m, b)
}
func (m *FileInfo) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_FileInfo.Marshal(b, m, deterministic)
}
func (dst *FileInfo) XXX_Merge(src proto.Message) {
	xxx_messageInfo_FileInfo.Merge(dst, src)
}
func (m *FileInfo) XXX_Size() int {
	return xxx_messageInfo_FileInfo.Size(m)
}
func (m *FileInfo) XXX_DiscardUnknown() {
	xxx_messageInfo_FileInfo.DiscardUnknown(m)
}

var xxx_messageInfo_FileInfo proto.InternalMessageInfo

func (m *FileInfo) GetName() string {
	if m != nil {
		return m.Name
	}
	return ""
}

func (m *FileInfo) GetMode() uint32 {
	if m != nil {
		return m.Mode
	}
	return 0
}

func (m *FileInfo) GetUser() string {
	if m != nil {
		return m.User
	}
	return ""
}

func (m *FileInfo) GetGroup() string {
	if m != nil {
		return m.Group
	}
	return ""
}

func (m *FileInfo) GetTimestamp() int64 {
	if m != nil {
		return m.Timestamp
	}
	return 0
}

func (m *FileInfo) GetSize() int64 {
	if m != nil {
		return m.Size
	}
	return 0
}

func (m *FileInfo) GetTree() bool {
	if m != nil {
		return m.Tree
	}
	return false
}

type File struct {
	// an ordered list of file parts
	Parts []*FilePart `protobuf:"bytes,1,rep,name=parts,proto3" json:"parts,omitempty"`
	// an ordered list of references to other file objects
	Splits               []*Ref   `protobuf:"bytes,2,rep,name=splits,proto3" json:"splits,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *File) Reset()         { *m = File{} }
func (m *File) String() string { return proto.CompactTextString(m) }
func (*File) ProtoMessage()    {}
func (*File) Descriptor() ([]byte, []int) {
	return fileDescriptor_file_1ca139b73b0b924b, []int{1}
}
func (m *File) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_File.Unmarshal(m, b)
}
func (m *File) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_File.Marshal(b, m, deterministic)
}
func (dst *File) XXX_Merge(src proto.Message) {
	xxx_messageInfo_File.Merge(dst, src)
}
func (m *File) XXX_Size() int {
	return xxx_messageInfo_File.Size(m)
}
func (m *File) XXX_DiscardUnknown() {
	xxx_messageInfo_File.DiscardUnknown(m)
}

var xxx_messageInfo_File proto.InternalMessageInfo

func (m *File) GetParts() []*FilePart {
	if m != nil {
		return m.Parts
	}
	return nil
}

func (m *File) GetSplits() []*Ref {
	if m != nil {
		return m.Splits
	}
	return nil
}

type FilePart struct {
	Offset               uint64   `protobuf:"varint,1,opt,name=offset,proto3" json:"offset,omitempty"`
	Length               uint64   `protobuf:"varint,2,opt,name=length,proto3" json:"length,omitempty"`
	Ref                  *Ref     `protobuf:"bytes,3,opt,name=ref,proto3" json:"ref,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *FilePart) Reset()         { *m = FilePart{} }
func (m *FilePart) String() string { return proto.CompactTextString(m) }
func (*FilePart) ProtoMessage()    {}
func (*FilePart) Descriptor() ([]byte, []int) {
	return fileDescriptor_file_1ca139b73b0b924b, []int{2}
}
func (m *FilePart) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_FilePart.Unmarshal(m, b)
}
func (m *FilePart) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_FilePart.Marshal(b, m, deterministic)
}
func (dst *FilePart) XXX_Merge(src proto.Message) {
	xxx_messageInfo_FilePart.Merge(dst, src)
}
func (m *FilePart) XXX_Size() int {
	return xxx_messageInfo_FilePart.Size(m)
}
func (m *FilePart) XXX_DiscardUnknown() {
	xxx_messageInfo_FilePart.DiscardUnknown(m)
}

var xxx_messageInfo_FilePart proto.InternalMessageInfo

func (m *FilePart) GetOffset() uint64 {
	if m != nil {
		return m.Offset
	}
	return 0
}

func (m *FilePart) GetLength() uint64 {
	if m != nil {
		return m.Length
	}
	return 0
}

func (m *FilePart) GetRef() *Ref {
	if m != nil {
		return m.Ref
	}
	return nil
}

func init() {
	proto.RegisterType((*FileInfo)(nil), "proto.FileInfo")
	proto.RegisterType((*File)(nil), "proto.File")
	proto.RegisterType((*FilePart)(nil), "proto.FilePart")
}

func init() { proto.RegisterFile("file.proto", fileDescriptor_file_1ca139b73b0b924b) }

var fileDescriptor_file_1ca139b73b0b924b = []byte{
	// 253 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x54, 0x8f, 0x4f, 0x4b, 0xc4, 0x30,
	0x10, 0xc5, 0xc9, 0xf6, 0x8f, 0xdb, 0x11, 0x11, 0x82, 0x48, 0x90, 0x3d, 0x84, 0x82, 0x90, 0xd3,
	0x1e, 0xf4, 0x3b, 0x08, 0xde, 0x34, 0x27, 0xaf, 0x15, 0x27, 0x6b, 0xa0, 0x6d, 0x42, 0x92, 0xbd,
	0xf8, 0x71, 0xfc, 0xa4, 0x32, 0x93, 0x82, 0xec, 0xa9, 0xef, 0xfd, 0xde, 0xeb, 0x90, 0x07, 0xe0,
	0xfc, 0x8c, 0xc7, 0x98, 0x42, 0x09, 0xb2, 0xe3, 0xcf, 0xc3, 0x90, 0xd0, 0x55, 0x32, 0xfe, 0x0a,
	0xd8, 0xbf, 0xf8, 0x19, 0x5f, 0x57, 0x17, 0xa4, 0x84, 0x76, 0x9d, 0x16, 0x54, 0x42, 0x0b, 0x33,
	0x58, 0xd6, 0xc4, 0x96, 0xf0, 0x85, 0x6a, 0xa7, 0x85, 0xb9, 0xb1, 0xac, 0x89, 0x9d, 0x33, 0x26,
	0xd5, 0xd4, 0x1e, 0x69, 0x79, 0x07, 0xdd, 0x29, 0x85, 0x73, 0x54, 0x2d, 0xc3, 0x6a, 0xe4, 0x01,
	0x86, 0xe2, 0x17, 0xcc, 0x65, 0x5a, 0xa2, 0xea, 0xb4, 0x30, 0x8d, 0xfd, 0x07, 0x74, 0x27, 0xfb,
	0x1f, 0x54, 0x3d, 0x07, 0xac, 0x89, 0x95, 0x84, 0xa8, 0xae, 0xb4, 0x30, 0x7b, 0xcb, 0x7a, 0x7c,
	0x87, 0x96, 0xde, 0x28, 0x1f, 0xa1, 0x8b, 0x53, 0x2a, 0x59, 0x09, 0xdd, 0x98, 0xeb, 0xa7, 0xdb,
	0xba, 0xe1, 0x48, 0xd9, 0xdb, 0x94, 0x8a, 0xad, 0xa9, 0x1c, 0xa1, 0xcf, 0x71, 0xf6, 0x25, 0xab,
	0x1d, 0xf7, 0x60, 0xeb, 0x59, 0x74, 0x76, 0x4b, 0xc6, 0x8f, 0x3a, 0x9b, 0x7e, 0x93, 0xf7, 0xd0,
	0x07, 0xe7, 0x32, 0x16, 0x1e, 0xde, 0xda, 0xcd, 0x11, 0x9f, 0x71, 0x3d, 0x95, 0x6f, 0x1e, 0xdf,
	0xda, 0xcd, 0xc9, 0x03, 0x34, 0x09, 0x1d, 0xaf, 0xbf, 0x3c, 0x4e, 0xf8, 0xb3, 0x67, 0xff, 0xfc,
	0x17, 0x00, 0x00, 0xff, 0xff, 0x41, 0x32, 0x78, 0x02, 0x78, 0x01, 0x00, 0x00,
}
