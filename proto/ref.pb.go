// Code generated by protoc-gen-go.
// source: ref.proto
// DO NOT EDIT!

package proto

import proto1 "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto1.Marshal
var _ = fmt.Errorf
var _ = math.Inf

type Ref struct {
	Sha1 []byte `protobuf:"bytes,1,opt,name=sha1,proto3" json:"sha1,omitempty"`
}

func (m *Ref) Reset()                    { *m = Ref{} }
func (m *Ref) String() string            { return proto1.CompactTextString(m) }
func (*Ref) ProtoMessage()               {}
func (*Ref) Descriptor() ([]byte, []int) { return fileDescriptor5, []int{0} }

func init() {
	proto1.RegisterType((*Ref)(nil), "proto.Ref")
}

var fileDescriptor5 = []byte{
	// 67 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0xe2, 0xe2, 0x2c, 0x4a, 0x4d, 0xd3,
	0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0x62, 0x05, 0x53, 0x4a, 0x92, 0x5c, 0xcc, 0x41, 0xa9, 0x69,
	0x42, 0x42, 0x5c, 0x2c, 0xc5, 0x19, 0x89, 0x86, 0x12, 0x8c, 0x0a, 0x8c, 0x1a, 0x3c, 0x41, 0x60,
	0x76, 0x12, 0x1b, 0x58, 0x85, 0x31, 0x20, 0x00, 0x00, 0xff, 0xff, 0xd6, 0x49, 0x47, 0x6b, 0x35,
	0x00, 0x00, 0x00,
}
