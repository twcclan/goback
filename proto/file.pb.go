// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.27.1
// 	protoc        v3.15.7
// source: file.proto

package proto

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// this contains file metadata, which can be different
// accross backups, even if file content is the same
type FileInfo struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Name      string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Mode      uint32 `protobuf:"varint,2,opt,name=mode,proto3" json:"mode,omitempty"`
	User      string `protobuf:"bytes,3,opt,name=user,proto3" json:"user,omitempty"`
	Group     string `protobuf:"bytes,4,opt,name=group,proto3" json:"group,omitempty"`
	Timestamp int64  `protobuf:"varint,5,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
	Size      int64  `protobuf:"varint,6,opt,name=size,proto3" json:"size,omitempty"`
	Tree      bool   `protobuf:"varint,7,opt,name=tree,proto3" json:"tree,omitempty"`
}

func (x *FileInfo) Reset() {
	*x = FileInfo{}
	if protoimpl.UnsafeEnabled {
		mi := &file_file_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *FileInfo) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*FileInfo) ProtoMessage() {}

func (x *FileInfo) ProtoReflect() protoreflect.Message {
	mi := &file_file_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use FileInfo.ProtoReflect.Descriptor instead.
func (*FileInfo) Descriptor() ([]byte, []int) {
	return file_file_proto_rawDescGZIP(), []int{0}
}

func (x *FileInfo) GetName() string {
	if x != nil {
		return x.Name
	}
	return ""
}

func (x *FileInfo) GetMode() uint32 {
	if x != nil {
		return x.Mode
	}
	return 0
}

func (x *FileInfo) GetUser() string {
	if x != nil {
		return x.User
	}
	return ""
}

func (x *FileInfo) GetGroup() string {
	if x != nil {
		return x.Group
	}
	return ""
}

func (x *FileInfo) GetTimestamp() int64 {
	if x != nil {
		return x.Timestamp
	}
	return 0
}

func (x *FileInfo) GetSize() int64 {
	if x != nil {
		return x.Size
	}
	return 0
}

func (x *FileInfo) GetTree() bool {
	if x != nil {
		return x.Tree
	}
	return false
}

type File struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// an ordered list of file parts
	Parts []*FilePart `protobuf:"bytes,1,rep,name=parts,proto3" json:"parts,omitempty"`
	// an ordered list of references to other file objects
	Splits []*Ref `protobuf:"bytes,2,rep,name=splits,proto3" json:"splits,omitempty"`
}

func (x *File) Reset() {
	*x = File{}
	if protoimpl.UnsafeEnabled {
		mi := &file_file_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *File) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*File) ProtoMessage() {}

func (x *File) ProtoReflect() protoreflect.Message {
	mi := &file_file_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use File.ProtoReflect.Descriptor instead.
func (*File) Descriptor() ([]byte, []int) {
	return file_file_proto_rawDescGZIP(), []int{1}
}

func (x *File) GetParts() []*FilePart {
	if x != nil {
		return x.Parts
	}
	return nil
}

func (x *File) GetSplits() []*Ref {
	if x != nil {
		return x.Splits
	}
	return nil
}

type FilePart struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Offset uint64 `protobuf:"varint,1,opt,name=offset,proto3" json:"offset,omitempty"`
	Length uint64 `protobuf:"varint,2,opt,name=length,proto3" json:"length,omitempty"`
	Ref    *Ref   `protobuf:"bytes,3,opt,name=ref,proto3" json:"ref,omitempty"`
}

func (x *FilePart) Reset() {
	*x = FilePart{}
	if protoimpl.UnsafeEnabled {
		mi := &file_file_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *FilePart) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*FilePart) ProtoMessage() {}

func (x *FilePart) ProtoReflect() protoreflect.Message {
	mi := &file_file_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use FilePart.ProtoReflect.Descriptor instead.
func (*FilePart) Descriptor() ([]byte, []int) {
	return file_file_proto_rawDescGZIP(), []int{2}
}

func (x *FilePart) GetOffset() uint64 {
	if x != nil {
		return x.Offset
	}
	return 0
}

func (x *FilePart) GetLength() uint64 {
	if x != nil {
		return x.Length
	}
	return 0
}

func (x *FilePart) GetRef() *Ref {
	if x != nil {
		return x.Ref
	}
	return nil
}

var File_file_proto protoreflect.FileDescriptor

var file_file_proto_rawDesc = []byte{
	0x0a, 0x0a, 0x66, 0x69, 0x6c, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x05, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x1a, 0x09, 0x72, 0x65, 0x66, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0xa2,
	0x01, 0x0a, 0x08, 0x46, 0x69, 0x6c, 0x65, 0x49, 0x6e, 0x66, 0x6f, 0x12, 0x12, 0x0a, 0x04, 0x6e,
	0x61, 0x6d, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x6e, 0x61, 0x6d, 0x65, 0x12,
	0x12, 0x0a, 0x04, 0x6d, 0x6f, 0x64, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0d, 0x52, 0x04, 0x6d,
	0x6f, 0x64, 0x65, 0x12, 0x12, 0x0a, 0x04, 0x75, 0x73, 0x65, 0x72, 0x18, 0x03, 0x20, 0x01, 0x28,
	0x09, 0x52, 0x04, 0x75, 0x73, 0x65, 0x72, 0x12, 0x14, 0x0a, 0x05, 0x67, 0x72, 0x6f, 0x75, 0x70,
	0x18, 0x04, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x67, 0x72, 0x6f, 0x75, 0x70, 0x12, 0x1c, 0x0a,
	0x09, 0x74, 0x69, 0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x18, 0x05, 0x20, 0x01, 0x28, 0x03,
	0x52, 0x09, 0x74, 0x69, 0x6d, 0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x12, 0x12, 0x0a, 0x04, 0x73,
	0x69, 0x7a, 0x65, 0x18, 0x06, 0x20, 0x01, 0x28, 0x03, 0x52, 0x04, 0x73, 0x69, 0x7a, 0x65, 0x12,
	0x12, 0x0a, 0x04, 0x74, 0x72, 0x65, 0x65, 0x18, 0x07, 0x20, 0x01, 0x28, 0x08, 0x52, 0x04, 0x74,
	0x72, 0x65, 0x65, 0x22, 0x51, 0x0a, 0x04, 0x46, 0x69, 0x6c, 0x65, 0x12, 0x25, 0x0a, 0x05, 0x70,
	0x61, 0x72, 0x74, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x0f, 0x2e, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x2e, 0x46, 0x69, 0x6c, 0x65, 0x50, 0x61, 0x72, 0x74, 0x52, 0x05, 0x70, 0x61, 0x72,
	0x74, 0x73, 0x12, 0x22, 0x0a, 0x06, 0x73, 0x70, 0x6c, 0x69, 0x74, 0x73, 0x18, 0x02, 0x20, 0x03,
	0x28, 0x0b, 0x32, 0x0a, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2e, 0x52, 0x65, 0x66, 0x52, 0x06,
	0x73, 0x70, 0x6c, 0x69, 0x74, 0x73, 0x22, 0x58, 0x0a, 0x08, 0x46, 0x69, 0x6c, 0x65, 0x50, 0x61,
	0x72, 0x74, 0x12, 0x16, 0x0a, 0x06, 0x6f, 0x66, 0x66, 0x73, 0x65, 0x74, 0x18, 0x01, 0x20, 0x01,
	0x28, 0x04, 0x52, 0x06, 0x6f, 0x66, 0x66, 0x73, 0x65, 0x74, 0x12, 0x16, 0x0a, 0x06, 0x6c, 0x65,
	0x6e, 0x67, 0x74, 0x68, 0x18, 0x02, 0x20, 0x01, 0x28, 0x04, 0x52, 0x06, 0x6c, 0x65, 0x6e, 0x67,
	0x74, 0x68, 0x12, 0x1c, 0x0a, 0x03, 0x72, 0x65, 0x66, 0x18, 0x03, 0x20, 0x01, 0x28, 0x0b, 0x32,
	0x0a, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2e, 0x52, 0x65, 0x66, 0x52, 0x03, 0x72, 0x65, 0x66,
	0x42, 0x21, 0x5a, 0x1f, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x74,
	0x77, 0x63, 0x63, 0x6c, 0x61, 0x6e, 0x2f, 0x67, 0x6f, 0x62, 0x61, 0x63, 0x6b, 0x2f, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_file_proto_rawDescOnce sync.Once
	file_file_proto_rawDescData = file_file_proto_rawDesc
)

func file_file_proto_rawDescGZIP() []byte {
	file_file_proto_rawDescOnce.Do(func() {
		file_file_proto_rawDescData = protoimpl.X.CompressGZIP(file_file_proto_rawDescData)
	})
	return file_file_proto_rawDescData
}

var file_file_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_file_proto_goTypes = []interface{}{
	(*FileInfo)(nil), // 0: proto.FileInfo
	(*File)(nil),     // 1: proto.File
	(*FilePart)(nil), // 2: proto.FilePart
	(*Ref)(nil),      // 3: proto.Ref
}
var file_file_proto_depIdxs = []int32{
	2, // 0: proto.File.parts:type_name -> proto.FilePart
	3, // 1: proto.File.splits:type_name -> proto.Ref
	3, // 2: proto.FilePart.ref:type_name -> proto.Ref
	3, // [3:3] is the sub-list for method output_type
	3, // [3:3] is the sub-list for method input_type
	3, // [3:3] is the sub-list for extension type_name
	3, // [3:3] is the sub-list for extension extendee
	0, // [0:3] is the sub-list for field type_name
}

func init() { file_file_proto_init() }
func file_file_proto_init() {
	if File_file_proto != nil {
		return
	}
	file_ref_proto_init()
	if !protoimpl.UnsafeEnabled {
		file_file_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*FileInfo); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_file_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*File); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_file_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*FilePart); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_file_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_file_proto_goTypes,
		DependencyIndexes: file_file_proto_depIdxs,
		MessageInfos:      file_file_proto_msgTypes,
	}.Build()
	File_file_proto = out.File
	file_file_proto_rawDesc = nil
	file_file_proto_goTypes = nil
	file_file_proto_depIdxs = nil
}
