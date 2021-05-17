// Code generated by protoc-gen-go. DO NOT EDIT.
// source: object.proto

package proto

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import timestamp "github.com/golang/protobuf/ptypes/timestamp"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type ObjectType int32

const (
	ObjectType_INVALID   ObjectType = 0
	ObjectType_COMMIT    ObjectType = 1
	ObjectType_TREE      ObjectType = 2
	ObjectType_FILE      ObjectType = 3
	ObjectType_BLOB      ObjectType = 4
	ObjectType_TOMBSTONE ObjectType = 5
)

var ObjectType_name = map[int32]string{
	0: "INVALID",
	1: "COMMIT",
	2: "TREE",
	3: "FILE",
	4: "BLOB",
	5: "TOMBSTONE",
}
var ObjectType_value = map[string]int32{
	"INVALID":   0,
	"COMMIT":    1,
	"TREE":      2,
	"FILE":      3,
	"BLOB":      4,
	"TOMBSTONE": 5,
}

func (x ObjectType) String() string {
	return proto.EnumName(ObjectType_name, int32(x))
}
func (ObjectType) EnumDescriptor() ([]byte, []int) {
	return fileDescriptor_object_d40858fe99cbcf5c, []int{0}
}

type Compression int32

const (
	Compression_NONE Compression = 0
	Compression_GZIP Compression = 1
)

var Compression_name = map[int32]string{
	0: "NONE",
	1: "GZIP",
}
var Compression_value = map[string]int32{
	"NONE": 0,
	"GZIP": 1,
}

func (x Compression) String() string {
	return proto.EnumName(Compression_name, int32(x))
}
func (Compression) EnumDescriptor() ([]byte, []int) {
	return fileDescriptor_object_d40858fe99cbcf5c, []int{1}
}

type ObjectHeader struct {
	Ref                  *Ref                 `protobuf:"bytes,1,opt,name=ref,proto3" json:"ref,omitempty"`
	Predecessor          *Ref                 `protobuf:"bytes,2,opt,name=predecessor,proto3" json:"predecessor,omitempty"`
	Size                 uint64               `protobuf:"varint,3,opt,name=size,proto3" json:"size,omitempty"`
	Compression          Compression          `protobuf:"varint,4,opt,name=compression,proto3,enum=proto.Compression" json:"compression,omitempty"`
	Timestamp            *timestamp.Timestamp `protobuf:"bytes,5,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
	Type                 ObjectType           `protobuf:"varint,6,opt,name=type,proto3,enum=proto.ObjectType" json:"type,omitempty"`
	TombstoneFor         *Ref                 `protobuf:"bytes,7,opt,name=tombstone_for,json=tombstoneFor,proto3" json:"tombstone_for,omitempty"`
	XXX_NoUnkeyedLiteral struct{}             `json:"-"`
	XXX_unrecognized     []byte               `json:"-"`
	XXX_sizecache        int32                `json:"-"`
}

func (m *ObjectHeader) Reset()         { *m = ObjectHeader{} }
func (m *ObjectHeader) String() string { return proto.CompactTextString(m) }
func (*ObjectHeader) ProtoMessage()    {}
func (*ObjectHeader) Descriptor() ([]byte, []int) {
	return fileDescriptor_object_d40858fe99cbcf5c, []int{0}
}
func (m *ObjectHeader) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ObjectHeader.Unmarshal(m, b)
}
func (m *ObjectHeader) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ObjectHeader.Marshal(b, m, deterministic)
}
func (dst *ObjectHeader) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ObjectHeader.Merge(dst, src)
}
func (m *ObjectHeader) XXX_Size() int {
	return xxx_messageInfo_ObjectHeader.Size(m)
}
func (m *ObjectHeader) XXX_DiscardUnknown() {
	xxx_messageInfo_ObjectHeader.DiscardUnknown(m)
}

var xxx_messageInfo_ObjectHeader proto.InternalMessageInfo

func (m *ObjectHeader) GetRef() *Ref {
	if m != nil {
		return m.Ref
	}
	return nil
}

func (m *ObjectHeader) GetPredecessor() *Ref {
	if m != nil {
		return m.Predecessor
	}
	return nil
}

func (m *ObjectHeader) GetSize() uint64 {
	if m != nil {
		return m.Size
	}
	return 0
}

func (m *ObjectHeader) GetCompression() Compression {
	if m != nil {
		return m.Compression
	}
	return Compression_NONE
}

func (m *ObjectHeader) GetTimestamp() *timestamp.Timestamp {
	if m != nil {
		return m.Timestamp
	}
	return nil
}

func (m *ObjectHeader) GetType() ObjectType {
	if m != nil {
		return m.Type
	}
	return ObjectType_INVALID
}

func (m *ObjectHeader) GetTombstoneFor() *Ref {
	if m != nil {
		return m.TombstoneFor
	}
	return nil
}

type Object struct {
	// Types that are valid to be assigned to Object:
	//	*Object_Commit
	//	*Object_Tree
	//	*Object_File
	//	*Object_Blob
	Object               isObject_Object `protobuf_oneof:"object"`
	XXX_NoUnkeyedLiteral struct{}        `json:"-"`
	XXX_unrecognized     []byte          `json:"-"`
	XXX_sizecache        int32           `json:"-"`
}

func (m *Object) Reset()         { *m = Object{} }
func (m *Object) String() string { return proto.CompactTextString(m) }
func (*Object) ProtoMessage()    {}
func (*Object) Descriptor() ([]byte, []int) {
	return fileDescriptor_object_d40858fe99cbcf5c, []int{1}
}
func (m *Object) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Object.Unmarshal(m, b)
}
func (m *Object) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Object.Marshal(b, m, deterministic)
}
func (dst *Object) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Object.Merge(dst, src)
}
func (m *Object) XXX_Size() int {
	return xxx_messageInfo_Object.Size(m)
}
func (m *Object) XXX_DiscardUnknown() {
	xxx_messageInfo_Object.DiscardUnknown(m)
}

var xxx_messageInfo_Object proto.InternalMessageInfo

type isObject_Object interface {
	isObject_Object()
}

type Object_Commit struct {
	Commit *Commit `protobuf:"bytes,1,opt,name=commit,proto3,oneof"`
}
type Object_Tree struct {
	Tree *Tree `protobuf:"bytes,2,opt,name=tree,proto3,oneof"`
}
type Object_File struct {
	File *File `protobuf:"bytes,3,opt,name=file,proto3,oneof"`
}
type Object_Blob struct {
	Blob *Blob `protobuf:"bytes,4,opt,name=blob,proto3,oneof"`
}

func (*Object_Commit) isObject_Object() {}
func (*Object_Tree) isObject_Object()   {}
func (*Object_File) isObject_Object()   {}
func (*Object_Blob) isObject_Object()   {}

func (m *Object) GetObject() isObject_Object {
	if m != nil {
		return m.Object
	}
	return nil
}

func (m *Object) GetCommit() *Commit {
	if x, ok := m.GetObject().(*Object_Commit); ok {
		return x.Commit
	}
	return nil
}

func (m *Object) GetTree() *Tree {
	if x, ok := m.GetObject().(*Object_Tree); ok {
		return x.Tree
	}
	return nil
}

func (m *Object) GetFile() *File {
	if x, ok := m.GetObject().(*Object_File); ok {
		return x.File
	}
	return nil
}

func (m *Object) GetBlob() *Blob {
	if x, ok := m.GetObject().(*Object_Blob); ok {
		return x.Blob
	}
	return nil
}

// XXX_OneofFuncs is for the internal use of the proto package.
func (*Object) XXX_OneofFuncs() (func(msg proto.Message, b *proto.Buffer) error, func(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error), func(msg proto.Message) (n int), []interface{}) {
	return _Object_OneofMarshaler, _Object_OneofUnmarshaler, _Object_OneofSizer, []interface{}{
		(*Object_Commit)(nil),
		(*Object_Tree)(nil),
		(*Object_File)(nil),
		(*Object_Blob)(nil),
	}
}

func _Object_OneofMarshaler(msg proto.Message, b *proto.Buffer) error {
	m := msg.(*Object)
	// object
	switch x := m.Object.(type) {
	case *Object_Commit:
		b.EncodeVarint(1<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.Commit); err != nil {
			return err
		}
	case *Object_Tree:
		b.EncodeVarint(2<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.Tree); err != nil {
			return err
		}
	case *Object_File:
		b.EncodeVarint(3<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.File); err != nil {
			return err
		}
	case *Object_Blob:
		b.EncodeVarint(4<<3 | proto.WireBytes)
		if err := b.EncodeMessage(x.Blob); err != nil {
			return err
		}
	case nil:
	default:
		return fmt.Errorf("Object.Object has unexpected type %T", x)
	}
	return nil
}

func _Object_OneofUnmarshaler(msg proto.Message, tag, wire int, b *proto.Buffer) (bool, error) {
	m := msg.(*Object)
	switch tag {
	case 1: // object.commit
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(Commit)
		err := b.DecodeMessage(msg)
		m.Object = &Object_Commit{msg}
		return true, err
	case 2: // object.tree
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(Tree)
		err := b.DecodeMessage(msg)
		m.Object = &Object_Tree{msg}
		return true, err
	case 3: // object.file
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(File)
		err := b.DecodeMessage(msg)
		m.Object = &Object_File{msg}
		return true, err
	case 4: // object.blob
		if wire != proto.WireBytes {
			return true, proto.ErrInternalBadWireType
		}
		msg := new(Blob)
		err := b.DecodeMessage(msg)
		m.Object = &Object_Blob{msg}
		return true, err
	default:
		return false, nil
	}
}

func _Object_OneofSizer(msg proto.Message) (n int) {
	m := msg.(*Object)
	// object
	switch x := m.Object.(type) {
	case *Object_Commit:
		s := proto.Size(x.Commit)
		n += 1 // tag and wire
		n += proto.SizeVarint(uint64(s))
		n += s
	case *Object_Tree:
		s := proto.Size(x.Tree)
		n += 1 // tag and wire
		n += proto.SizeVarint(uint64(s))
		n += s
	case *Object_File:
		s := proto.Size(x.File)
		n += 1 // tag and wire
		n += proto.SizeVarint(uint64(s))
		n += s
	case *Object_Blob:
		s := proto.Size(x.Blob)
		n += 1 // tag and wire
		n += proto.SizeVarint(uint64(s))
		n += s
	case nil:
	default:
		panic(fmt.Sprintf("proto: unexpected type %T in oneof", x))
	}
	return n
}

func init() {
	proto.RegisterType((*ObjectHeader)(nil), "proto.ObjectHeader")
	proto.RegisterType((*Object)(nil), "proto.Object")
	proto.RegisterEnum("proto.ObjectType", ObjectType_name, ObjectType_value)
	proto.RegisterEnum("proto.Compression", Compression_name, Compression_value)
}

func init() { proto.RegisterFile("object.proto", fileDescriptor_object_d40858fe99cbcf5c) }

var fileDescriptor_object_d40858fe99cbcf5c = []byte{
	// 430 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x64, 0x92, 0x41, 0x6f, 0x9b, 0x40,
	0x10, 0x85, 0x8d, 0x8d, 0x49, 0x3c, 0xd8, 0x15, 0x9d, 0x13, 0xb2, 0x2a, 0xd5, 0x89, 0x54, 0xd5,
	0x8a, 0x2a, 0x2c, 0xa5, 0x3d, 0xf4, 0x5a, 0x52, 0x5c, 0x23, 0xd9, 0xa6, 0xda, 0xa2, 0x1e, 0x7a,
	0xa9, 0x02, 0x19, 0x22, 0x2a, 0xc8, 0xa2, 0x65, 0x7b, 0x48, 0x7f, 0x4f, 0x7f, 0x43, 0x7f, 0x5f,
	0xb5, 0xbb, 0x10, 0x2c, 0xe5, 0xc4, 0x83, 0xf7, 0x69, 0x87, 0xf7, 0x66, 0x61, 0xce, 0xb3, 0x5f,
	0x94, 0xcb, 0xa0, 0x11, 0x5c, 0x72, 0x9c, 0xea, 0xc7, 0xf2, 0xf5, 0x3d, 0xe7, 0xf7, 0x15, 0x6d,
	0xf4, 0x5b, 0xf6, 0xbb, 0xd8, 0xc8, 0xb2, 0xa6, 0x56, 0xde, 0xd6, 0x8d, 0xe1, 0x96, 0x20, 0x05,
	0x51, 0xa7, 0xe7, 0x39, 0xaf, 0xeb, 0x52, 0xf6, 0x4e, 0x56, 0xf1, 0xac, 0xd7, 0x45, 0x59, 0xf5,
	0xd4, 0x4c, 0x50, 0x61, 0xe4, 0xe5, 0xbf, 0x31, 0xcc, 0x13, 0x3d, 0x75, 0x47, 0xb7, 0x77, 0x24,
	0xf0, 0x15, 0x4c, 0x04, 0x15, 0xbe, 0xb5, 0xb2, 0xd6, 0xee, 0x35, 0x18, 0x2a, 0x60, 0x54, 0x30,
	0xf5, 0x19, 0xdf, 0x81, 0xdb, 0x08, 0xba, 0xa3, 0x9c, 0xda, 0x96, 0x0b, 0x7f, 0xfc, 0x8c, 0x3a,
	0xb5, 0x11, 0xc1, 0x6e, 0xcb, 0x3f, 0xe4, 0x4f, 0x56, 0xd6, 0xda, 0x66, 0x5a, 0xe3, 0x07, 0x70,
	0x73, 0x5e, 0x37, 0x82, 0xda, 0xb6, 0xe4, 0x0f, 0xbe, 0xbd, 0xb2, 0xd6, 0x2f, 0xae, 0xb1, 0x3b,
	0xe1, 0x66, 0x70, 0xd8, 0x29, 0x86, 0x1f, 0x61, 0xf6, 0x14, 0xdb, 0x9f, 0xea, 0xa9, 0xcb, 0xc0,
	0x14, 0x13, 0xf4, 0xc5, 0x04, 0x69, 0x4f, 0xb0, 0x01, 0xc6, 0x37, 0x60, 0xcb, 0xc7, 0x86, 0x7c,
	0x47, 0x0f, 0x7a, 0xd9, 0x0d, 0x32, 0x91, 0xd3, 0xc7, 0x86, 0x98, 0xb6, 0x71, 0x03, 0x0b, 0xc9,
	0xeb, 0xac, 0x95, 0xfc, 0x81, 0x7e, 0x16, 0x5c, 0xf8, 0x67, 0xcf, 0xa2, 0xcd, 0x9f, 0x80, 0x2d,
	0x17, 0x97, 0x7f, 0x2d, 0x70, 0xcc, 0x29, 0xf8, 0x16, 0x1c, 0x53, 0x7b, 0xd7, 0xda, 0x62, 0x48,
	0x53, 0x97, 0x72, 0x37, 0x62, 0x9d, 0x8d, 0x17, 0x60, 0xab, 0x5d, 0x75, 0xb5, 0xb9, 0x1d, 0x96,
	0x0a, 0xa2, 0xdd, 0x88, 0x69, 0x4b, 0x21, 0x6a, 0x51, 0xba, 0xb2, 0x01, 0xd9, 0x96, 0x95, 0x46,
	0x94, 0xa5, 0x10, 0xb5, 0x57, 0x5d, 0xdd, 0x80, 0x84, 0x15, 0xcf, 0x14, 0xa2, 0xac, 0xf0, 0x1c,
	0x1c, 0x73, 0x95, 0xae, 0x18, 0xc0, 0x90, 0x15, 0x5d, 0x38, 0x8b, 0x8f, 0xdf, 0x3f, 0xed, 0xe3,
	0xcf, 0xde, 0x08, 0x01, 0x9c, 0x9b, 0xe4, 0x70, 0x88, 0x53, 0xcf, 0xc2, 0x73, 0xb0, 0x53, 0x16,
	0x45, 0xde, 0x58, 0xa9, 0x6d, 0xbc, 0x8f, 0xbc, 0x89, 0x52, 0xe1, 0x3e, 0x09, 0x3d, 0x1b, 0x17,
	0x30, 0x4b, 0x93, 0x43, 0xf8, 0x2d, 0x4d, 0x8e, 0x91, 0x37, 0xbd, 0xba, 0x00, 0xf7, 0x64, 0x51,
	0x8a, 0x3b, 0x2a, 0x63, 0xa4, 0xd4, 0x97, 0x1f, 0xf1, 0x57, 0xcf, 0xca, 0x1c, 0xfd, 0x53, 0xef,
	0xff, 0x07, 0x00, 0x00, 0xff, 0xff, 0x4e, 0xe3, 0xed, 0xcb, 0xd2, 0x02, 0x00, 0x00,
}
