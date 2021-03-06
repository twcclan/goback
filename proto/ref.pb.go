// Code generated by protoc-gen-go. DO NOT EDIT.
// source: ref.proto

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

type Ref struct {
	Sha1                 []byte   `protobuf:"bytes,1,opt,name=sha1,proto3" json:"sha1,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *Ref) Reset()         { *m = Ref{} }
func (m *Ref) String() string { return proto.CompactTextString(m) }
func (*Ref) ProtoMessage()    {}
func (*Ref) Descriptor() ([]byte, []int) {
	return fileDescriptor_ref_8f42aef1f53294e7, []int{0}
}
func (m *Ref) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Ref.Unmarshal(m, b)
}
func (m *Ref) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Ref.Marshal(b, m, deterministic)
}
func (dst *Ref) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Ref.Merge(dst, src)
}
func (m *Ref) XXX_Size() int {
	return xxx_messageInfo_Ref.Size(m)
}
func (m *Ref) XXX_DiscardUnknown() {
	xxx_messageInfo_Ref.DiscardUnknown(m)
}

var xxx_messageInfo_Ref proto.InternalMessageInfo

func (m *Ref) GetSha1() []byte {
	if m != nil {
		return m.Sha1
	}
	return nil
}

func init() {
	proto.RegisterType((*Ref)(nil), "proto.Ref")
}

func init() { proto.RegisterFile("ref.proto", fileDescriptor_ref_8f42aef1f53294e7) }

var fileDescriptor_ref_8f42aef1f53294e7 = []byte{
	// 67 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0xe2, 0x2c, 0x4a, 0x4d, 0xd3,
	0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0x62, 0x05, 0x53, 0x4a, 0x92, 0x5c, 0xcc, 0x41, 0xa9, 0x69,
	0x42, 0x42, 0x5c, 0x2c, 0xc5, 0x19, 0x89, 0x86, 0x12, 0x8c, 0x0a, 0x8c, 0x1a, 0x3c, 0x41, 0x60,
	0x76, 0x12, 0x1b, 0x58, 0x85, 0x31, 0x20, 0x00, 0x00, 0xff, 0xff, 0xd6, 0x49, 0x47, 0x6b, 0x35,
	0x00, 0x00, 0x00,
}
