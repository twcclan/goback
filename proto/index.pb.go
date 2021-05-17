// Code generated by protoc-gen-go. DO NOT EDIT.
// source: index.proto

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

type Location struct {
	Ref    *Ref       `protobuf:"bytes,1,opt,name=ref,proto3" json:"ref,omitempty"`
	Offset uint64     `protobuf:"varint,2,opt,name=offset,proto3" json:"offset,omitempty"`
	Type   ObjectType `protobuf:"varint,3,opt,name=type,proto3,enum=proto.ObjectType" json:"type,omitempty"`
	// total size of object including header
	Size                 uint64   `protobuf:"varint,4,opt,name=size,proto3" json:"size,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *Location) Reset()         { *m = Location{} }
func (m *Location) String() string { return proto.CompactTextString(m) }
func (*Location) ProtoMessage()    {}
func (*Location) Descriptor() ([]byte, []int) {
	return fileDescriptor_index_8f115a5e357b55cf, []int{0}
}
func (m *Location) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Location.Unmarshal(m, b)
}
func (m *Location) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Location.Marshal(b, m, deterministic)
}
func (dst *Location) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Location.Merge(dst, src)
}
func (m *Location) XXX_Size() int {
	return xxx_messageInfo_Location.Size(m)
}
func (m *Location) XXX_DiscardUnknown() {
	xxx_messageInfo_Location.DiscardUnknown(m)
}

var xxx_messageInfo_Location proto.InternalMessageInfo

func (m *Location) GetRef() *Ref {
	if m != nil {
		return m.Ref
	}
	return nil
}

func (m *Location) GetOffset() uint64 {
	if m != nil {
		return m.Offset
	}
	return 0
}

func (m *Location) GetType() ObjectType {
	if m != nil {
		return m.Type
	}
	return ObjectType_INVALID
}

func (m *Location) GetSize() uint64 {
	if m != nil {
		return m.Size
	}
	return 0
}

type Index struct {
	Locations            []*Location `protobuf:"bytes,1,rep,name=locations,proto3" json:"locations,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *Index) Reset()         { *m = Index{} }
func (m *Index) String() string { return proto.CompactTextString(m) }
func (*Index) ProtoMessage()    {}
func (*Index) Descriptor() ([]byte, []int) {
	return fileDescriptor_index_8f115a5e357b55cf, []int{1}
}
func (m *Index) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Index.Unmarshal(m, b)
}
func (m *Index) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Index.Marshal(b, m, deterministic)
}
func (dst *Index) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Index.Merge(dst, src)
}
func (m *Index) XXX_Size() int {
	return xxx_messageInfo_Index.Size(m)
}
func (m *Index) XXX_DiscardUnknown() {
	xxx_messageInfo_Index.DiscardUnknown(m)
}

var xxx_messageInfo_Index proto.InternalMessageInfo

func (m *Index) GetLocations() []*Location {
	if m != nil {
		return m.Locations
	}
	return nil
}

type IndexHeader struct {
	Count                uint64   `protobuf:"varint,1,opt,name=count,proto3" json:"count,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *IndexHeader) Reset()         { *m = IndexHeader{} }
func (m *IndexHeader) String() string { return proto.CompactTextString(m) }
func (*IndexHeader) ProtoMessage()    {}
func (*IndexHeader) Descriptor() ([]byte, []int) {
	return fileDescriptor_index_8f115a5e357b55cf, []int{2}
}
func (m *IndexHeader) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_IndexHeader.Unmarshal(m, b)
}
func (m *IndexHeader) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_IndexHeader.Marshal(b, m, deterministic)
}
func (dst *IndexHeader) XXX_Merge(src proto.Message) {
	xxx_messageInfo_IndexHeader.Merge(dst, src)
}
func (m *IndexHeader) XXX_Size() int {
	return xxx_messageInfo_IndexHeader.Size(m)
}
func (m *IndexHeader) XXX_DiscardUnknown() {
	xxx_messageInfo_IndexHeader.DiscardUnknown(m)
}

var xxx_messageInfo_IndexHeader proto.InternalMessageInfo

func (m *IndexHeader) GetCount() uint64 {
	if m != nil {
		return m.Count
	}
	return 0
}

func init() {
	proto.RegisterType((*Location)(nil), "proto.Location")
	proto.RegisterType((*Index)(nil), "proto.Index")
	proto.RegisterType((*IndexHeader)(nil), "proto.IndexHeader")
}

func init() { proto.RegisterFile("index.proto", fileDescriptor_index_8f115a5e357b55cf) }

var fileDescriptor_index_8f115a5e357b55cf = []byte{
	// 213 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x44, 0x8e, 0xc1, 0x4a, 0xc4, 0x30,
	0x10, 0x86, 0x89, 0x4d, 0x17, 0x77, 0x22, 0x8a, 0x83, 0x48, 0x58, 0x3c, 0x84, 0x8a, 0x90, 0x8b,
	0x7b, 0xa8, 0xe0, 0x33, 0x28, 0x08, 0x42, 0xf0, 0x05, 0x76, 0xdb, 0x09, 0x54, 0xa4, 0x29, 0x69,
	0x04, 0xab, 0x2f, 0x2f, 0x9d, 0x44, 0x3c, 0x25, 0xff, 0xe4, 0xff, 0x32, 0x1f, 0xa8, 0x61, 0xec,
	0xe9, 0x6b, 0x3f, 0xc5, 0x90, 0x02, 0xd6, 0x7c, 0xec, 0xb6, 0x91, 0x7c, 0x9e, 0xec, 0xce, 0xc2,
	0xf1, 0x9d, 0xba, 0x94, 0x53, 0xf3, 0x03, 0xa7, 0x2f, 0xa1, 0x3b, 0xa4, 0x21, 0x8c, 0x78, 0x03,
	0x55, 0x24, 0xaf, 0x85, 0x11, 0x56, 0xb5, 0x90, 0x0b, 0x7b, 0x47, 0xde, 0xad, 0x63, 0xbc, 0x86,
	0x4d, 0xf0, 0x7e, 0xa6, 0xa4, 0x4f, 0x8c, 0xb0, 0xd2, 0x95, 0x84, 0x77, 0x20, 0xd3, 0x32, 0x91,
	0xae, 0x8c, 0xb0, 0xe7, 0xed, 0x65, 0xc1, 0x5e, 0x79, 0xc9, 0xdb, 0x32, 0x91, 0xe3, 0x67, 0x44,
	0x90, 0xf3, 0xf0, 0x4d, 0x5a, 0x32, 0xcc, 0xf7, 0xe6, 0x11, 0xea, 0xe7, 0xd5, 0x15, 0xef, 0x61,
	0xfb, 0x51, 0x2c, 0x66, 0x2d, 0x4c, 0x65, 0x55, 0x7b, 0x51, 0x3e, 0xfa, 0xb3, 0x73, 0xff, 0x8d,
	0xe6, 0x16, 0x14, 0x73, 0x4f, 0x74, 0xe8, 0x29, 0xe2, 0x15, 0xd4, 0x5d, 0xf8, 0x1c, 0x13, 0x9b,
	0x4b, 0x97, 0xc3, 0x71, 0xc3, 0xfc, 0xc3, 0x6f, 0x00, 0x00, 0x00, 0xff, 0xff, 0x70, 0xbb, 0x39,
	0xeb, 0x0f, 0x01, 0x00, 0x00,
}
