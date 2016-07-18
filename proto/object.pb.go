// Code generated by protoc-gen-go.
// source: object.proto
// DO NOT EDIT!

package proto

import proto1 "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto1.Marshal
var _ = fmt.Errorf
var _ = math.Inf

type ObjectType int32

const (
	ObjectType_INVALID ObjectType = 0
	ObjectType_COMMIT  ObjectType = 1
	ObjectType_TREE    ObjectType = 2
	ObjectType_FILE    ObjectType = 3
	ObjectType_BLOB    ObjectType = 4
)

var ObjectType_name = map[int32]string{
	0: "INVALID",
	1: "COMMIT",
	2: "TREE",
	3: "FILE",
	4: "BLOB",
}
var ObjectType_value = map[string]int32{
	"INVALID": 0,
	"COMMIT":  1,
	"TREE":    2,
	"FILE":    3,
	"BLOB":    4,
}

func (x ObjectType) String() string {
	return proto1.EnumName(ObjectType_name, int32(x))
}
func (ObjectType) EnumDescriptor() ([]byte, []int) { return fileDescriptor5, []int{0} }

type Object struct {
	// Types that are valid to be assigned to Object:
	//	*Object_Commit
	//	*Object_Tree
	//	*Object_File
	//	*Object_Blob
	Object isObject_Object `protobuf_oneof:"object"`
}

func (m *Object) Reset()                    { *m = Object{} }
func (m *Object) String() string            { return proto1.CompactTextString(m) }
func (*Object) ProtoMessage()               {}
func (*Object) Descriptor() ([]byte, []int) { return fileDescriptor5, []int{0} }

type isObject_Object interface {
	isObject_Object()
}

type Object_Commit struct {
	Commit *Commit `protobuf:"bytes,1,opt,name=commit,oneof"`
}
type Object_Tree struct {
	Tree *Tree `protobuf:"bytes,2,opt,name=tree,oneof"`
}
type Object_File struct {
	File *File `protobuf:"bytes,3,opt,name=file,oneof"`
}
type Object_Blob struct {
	Blob *Blob `protobuf:"bytes,4,opt,name=blob,oneof"`
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
func (*Object) XXX_OneofFuncs() (func(msg proto1.Message, b *proto1.Buffer) error, func(msg proto1.Message, tag, wire int, b *proto1.Buffer) (bool, error), func(msg proto1.Message) (n int), []interface{}) {
	return _Object_OneofMarshaler, _Object_OneofUnmarshaler, _Object_OneofSizer, []interface{}{
		(*Object_Commit)(nil),
		(*Object_Tree)(nil),
		(*Object_File)(nil),
		(*Object_Blob)(nil),
	}
}

func _Object_OneofMarshaler(msg proto1.Message, b *proto1.Buffer) error {
	m := msg.(*Object)
	// object
	switch x := m.Object.(type) {
	case *Object_Commit:
		b.EncodeVarint(1<<3 | proto1.WireBytes)
		if err := b.EncodeMessage(x.Commit); err != nil {
			return err
		}
	case *Object_Tree:
		b.EncodeVarint(2<<3 | proto1.WireBytes)
		if err := b.EncodeMessage(x.Tree); err != nil {
			return err
		}
	case *Object_File:
		b.EncodeVarint(3<<3 | proto1.WireBytes)
		if err := b.EncodeMessage(x.File); err != nil {
			return err
		}
	case *Object_Blob:
		b.EncodeVarint(4<<3 | proto1.WireBytes)
		if err := b.EncodeMessage(x.Blob); err != nil {
			return err
		}
	case nil:
	default:
		return fmt.Errorf("Object.Object has unexpected type %T", x)
	}
	return nil
}

func _Object_OneofUnmarshaler(msg proto1.Message, tag, wire int, b *proto1.Buffer) (bool, error) {
	m := msg.(*Object)
	switch tag {
	case 1: // object.commit
		if wire != proto1.WireBytes {
			return true, proto1.ErrInternalBadWireType
		}
		msg := new(Commit)
		err := b.DecodeMessage(msg)
		m.Object = &Object_Commit{msg}
		return true, err
	case 2: // object.tree
		if wire != proto1.WireBytes {
			return true, proto1.ErrInternalBadWireType
		}
		msg := new(Tree)
		err := b.DecodeMessage(msg)
		m.Object = &Object_Tree{msg}
		return true, err
	case 3: // object.file
		if wire != proto1.WireBytes {
			return true, proto1.ErrInternalBadWireType
		}
		msg := new(File)
		err := b.DecodeMessage(msg)
		m.Object = &Object_File{msg}
		return true, err
	case 4: // object.blob
		if wire != proto1.WireBytes {
			return true, proto1.ErrInternalBadWireType
		}
		msg := new(Blob)
		err := b.DecodeMessage(msg)
		m.Object = &Object_Blob{msg}
		return true, err
	default:
		return false, nil
	}
}

func _Object_OneofSizer(msg proto1.Message) (n int) {
	m := msg.(*Object)
	// object
	switch x := m.Object.(type) {
	case *Object_Commit:
		s := proto1.Size(x.Commit)
		n += proto1.SizeVarint(1<<3 | proto1.WireBytes)
		n += proto1.SizeVarint(uint64(s))
		n += s
	case *Object_Tree:
		s := proto1.Size(x.Tree)
		n += proto1.SizeVarint(2<<3 | proto1.WireBytes)
		n += proto1.SizeVarint(uint64(s))
		n += s
	case *Object_File:
		s := proto1.Size(x.File)
		n += proto1.SizeVarint(3<<3 | proto1.WireBytes)
		n += proto1.SizeVarint(uint64(s))
		n += s
	case *Object_Blob:
		s := proto1.Size(x.Blob)
		n += proto1.SizeVarint(4<<3 | proto1.WireBytes)
		n += proto1.SizeVarint(uint64(s))
		n += s
	case nil:
	default:
		panic(fmt.Sprintf("proto: unexpected type %T in oneof", x))
	}
	return n
}

func init() {
	proto1.RegisterType((*Object)(nil), "proto.Object")
	proto1.RegisterEnum("proto.ObjectType", ObjectType_name, ObjectType_value)
}

var fileDescriptor5 = []byte{
	// 226 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0x54, 0xcf, 0xc1, 0x4a, 0xc4, 0x30,
	0x10, 0x06, 0xe0, 0xcd, 0x6e, 0x8c, 0xcb, 0x74, 0x85, 0x90, 0x53, 0xe8, 0x49, 0xbd, 0x28, 0x1e,
	0xf6, 0xa0, 0x4f, 0x60, 0x6a, 0x17, 0x03, 0x5d, 0x0b, 0x4b, 0xf0, 0x01, 0x52, 0x22, 0x54, 0x5a,
	0x52, 0x4a, 0x2f, 0xbe, 0x8f, 0x0f, 0x6a, 0x26, 0x89, 0x87, 0x9e, 0x3a, 0xfc, 0xff, 0xd7, 0xce,
	0x14, 0x0e, 0xde, 0x7e, 0xbb, 0x6e, 0x39, 0x4e, 0xb3, 0x5f, 0xbc, 0xb8, 0x8a, 0x8f, 0x12, 0x96,
	0xd9, 0xb9, 0x14, 0x95, 0x87, 0xce, 0x8f, 0x63, 0x9f, 0x41, 0x09, 0x76, 0xf0, 0xf6, 0x7f, 0xfe,
	0xea, 0x87, 0xac, 0xee, 0x7f, 0x09, 0xb0, 0x36, 0x7e, 0x49, 0x3c, 0x00, 0x4b, 0xaf, 0x48, 0x72,
	0x4b, 0x1e, 0x8b, 0xe7, 0x9b, 0x44, 0x8e, 0x55, 0x0c, 0xdf, 0x37, 0x97, 0x5c, 0x8b, 0x3b, 0xa0,
	0xb8, 0x47, 0x6e, 0x23, 0x2b, 0x32, 0x33, 0x21, 0x0a, 0x28, 0x56, 0x48, 0x70, 0x89, 0xdc, 0xad,
	0xc8, 0x29, 0x44, 0x48, 0xb0, 0x42, 0x82, 0x37, 0x49, 0xba, 0x22, 0x2a, 0x44, 0x48, 0xb0, 0x52,
	0x7b, 0x60, 0xe9, 0x2f, 0x9f, 0x2a, 0x80, 0x74, 0xa5, 0xf9, 0x99, 0x9c, 0x28, 0xe0, 0x5a, 0x7f,
	0x7c, 0xbe, 0x36, 0xfa, 0x8d, 0x6f, 0x04, 0x00, 0xab, 0xda, 0xf3, 0x59, 0x1b, 0x4e, 0xc4, 0x1e,
	0xa8, 0xb9, 0xd4, 0x35, 0xdf, 0xe2, 0x74, 0xd2, 0x4d, 0xcd, 0x77, 0x38, 0xa9, 0xa6, 0x55, 0x9c,
	0x5a, 0x16, 0x57, 0xbc, 0xfc, 0x05, 0x00, 0x00, 0xff, 0xff, 0xcc, 0x27, 0x8e, 0xca, 0x3b, 0x01,
	0x00, 0x00,
}