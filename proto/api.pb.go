// Code generated by protoc-gen-go. DO NOT EDIT.
// source: api.proto

/*
Package proto is a generated protocol buffer package.

It is generated from these files:
	api.proto
	blob.proto
	commit.proto
	file.proto
	index.proto
	object.proto
	ref.proto
	tree.proto

It has these top-level messages:
	PutRequest
	PutResponse
	GetRequest
	GetResponse
	DeleteRequest
	DeleteResponse
	WalkRequest
	WalkResponse
	Blob
	Commit
	FileInfo
	File
	FilePart
	Location
	Index
	IndexHeader
	ObjectHeader
	Object
	Ref
	Tree
	TreeNode
*/
package proto

import proto1 "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto1.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto1.ProtoPackageIsVersion2 // please upgrade the proto package

// Put(*proto.Object) error
type PutRequest struct {
	Object *Object `protobuf:"bytes,1,opt,name=object" json:"object,omitempty"`
}

func (m *PutRequest) Reset()                    { *m = PutRequest{} }
func (m *PutRequest) String() string            { return proto1.CompactTextString(m) }
func (*PutRequest) ProtoMessage()               {}
func (*PutRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

func (m *PutRequest) GetObject() *Object {
	if m != nil {
		return m.Object
	}
	return nil
}

type PutResponse struct {
}

func (m *PutResponse) Reset()                    { *m = PutResponse{} }
func (m *PutResponse) String() string            { return proto1.CompactTextString(m) }
func (*PutResponse) ProtoMessage()               {}
func (*PutResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

// Get(*proto.Ref) (*proto.Object, error)
type GetRequest struct {
	Ref *Ref `protobuf:"bytes,1,opt,name=ref" json:"ref,omitempty"`
}

func (m *GetRequest) Reset()                    { *m = GetRequest{} }
func (m *GetRequest) String() string            { return proto1.CompactTextString(m) }
func (*GetRequest) ProtoMessage()               {}
func (*GetRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

func (m *GetRequest) GetRef() *Ref {
	if m != nil {
		return m.Ref
	}
	return nil
}

type GetResponse struct {
	Object *Object `protobuf:"bytes,1,opt,name=object" json:"object,omitempty"`
}

func (m *GetResponse) Reset()                    { *m = GetResponse{} }
func (m *GetResponse) String() string            { return proto1.CompactTextString(m) }
func (*GetResponse) ProtoMessage()               {}
func (*GetResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{3} }

func (m *GetResponse) GetObject() *Object {
	if m != nil {
		return m.Object
	}
	return nil
}

// Delete(*proto.Ref) error
type DeleteRequest struct {
}

func (m *DeleteRequest) Reset()                    { *m = DeleteRequest{} }
func (m *DeleteRequest) String() string            { return proto1.CompactTextString(m) }
func (*DeleteRequest) ProtoMessage()               {}
func (*DeleteRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{4} }

type DeleteResponse struct {
}

func (m *DeleteResponse) Reset()                    { *m = DeleteResponse{} }
func (m *DeleteResponse) String() string            { return proto1.CompactTextString(m) }
func (*DeleteResponse) ProtoMessage()               {}
func (*DeleteResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{5} }

// Walk(bool, proto.ObjectType, ObjectReceiver) error
type WalkRequest struct {
	Load       bool       `protobuf:"varint,1,opt,name=load" json:"load,omitempty"`
	ObjectType ObjectType `protobuf:"varint,2,opt,name=objectType,enum=proto.ObjectType" json:"objectType,omitempty"`
}

func (m *WalkRequest) Reset()                    { *m = WalkRequest{} }
func (m *WalkRequest) String() string            { return proto1.CompactTextString(m) }
func (*WalkRequest) ProtoMessage()               {}
func (*WalkRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{6} }

func (m *WalkRequest) GetLoad() bool {
	if m != nil {
		return m.Load
	}
	return false
}

func (m *WalkRequest) GetObjectType() ObjectType {
	if m != nil {
		return m.ObjectType
	}
	return ObjectType_INVALID
}

type WalkResponse struct {
	Header *ObjectHeader `protobuf:"bytes,1,opt,name=header" json:"header,omitempty"`
	Object *Object       `protobuf:"bytes,2,opt,name=object" json:"object,omitempty"`
}

func (m *WalkResponse) Reset()                    { *m = WalkResponse{} }
func (m *WalkResponse) String() string            { return proto1.CompactTextString(m) }
func (*WalkResponse) ProtoMessage()               {}
func (*WalkResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{7} }

func (m *WalkResponse) GetHeader() *ObjectHeader {
	if m != nil {
		return m.Header
	}
	return nil
}

func (m *WalkResponse) GetObject() *Object {
	if m != nil {
		return m.Object
	}
	return nil
}

func init() {
	proto1.RegisterType((*PutRequest)(nil), "proto.PutRequest")
	proto1.RegisterType((*PutResponse)(nil), "proto.PutResponse")
	proto1.RegisterType((*GetRequest)(nil), "proto.GetRequest")
	proto1.RegisterType((*GetResponse)(nil), "proto.GetResponse")
	proto1.RegisterType((*DeleteRequest)(nil), "proto.DeleteRequest")
	proto1.RegisterType((*DeleteResponse)(nil), "proto.DeleteResponse")
	proto1.RegisterType((*WalkRequest)(nil), "proto.WalkRequest")
	proto1.RegisterType((*WalkResponse)(nil), "proto.WalkResponse")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for Store service

type StoreClient interface {
	Put(ctx context.Context, in *PutRequest, opts ...grpc.CallOption) (*PutResponse, error)
	Get(ctx context.Context, in *GetRequest, opts ...grpc.CallOption) (*GetResponse, error)
	Delete(ctx context.Context, in *DeleteRequest, opts ...grpc.CallOption) (*DeleteResponse, error)
	Walk(ctx context.Context, in *WalkRequest, opts ...grpc.CallOption) (Store_WalkClient, error)
}

type storeClient struct {
	cc *grpc.ClientConn
}

func NewStoreClient(cc *grpc.ClientConn) StoreClient {
	return &storeClient{cc}
}

func (c *storeClient) Put(ctx context.Context, in *PutRequest, opts ...grpc.CallOption) (*PutResponse, error) {
	out := new(PutResponse)
	err := grpc.Invoke(ctx, "/proto.Store/Put", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *storeClient) Get(ctx context.Context, in *GetRequest, opts ...grpc.CallOption) (*GetResponse, error) {
	out := new(GetResponse)
	err := grpc.Invoke(ctx, "/proto.Store/Get", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *storeClient) Delete(ctx context.Context, in *DeleteRequest, opts ...grpc.CallOption) (*DeleteResponse, error) {
	out := new(DeleteResponse)
	err := grpc.Invoke(ctx, "/proto.Store/Delete", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *storeClient) Walk(ctx context.Context, in *WalkRequest, opts ...grpc.CallOption) (Store_WalkClient, error) {
	stream, err := grpc.NewClientStream(ctx, &_Store_serviceDesc.Streams[0], c.cc, "/proto.Store/Walk", opts...)
	if err != nil {
		return nil, err
	}
	x := &storeWalkClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Store_WalkClient interface {
	Recv() (*WalkResponse, error)
	grpc.ClientStream
}

type storeWalkClient struct {
	grpc.ClientStream
}

func (x *storeWalkClient) Recv() (*WalkResponse, error) {
	m := new(WalkResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Server API for Store service

type StoreServer interface {
	Put(context.Context, *PutRequest) (*PutResponse, error)
	Get(context.Context, *GetRequest) (*GetResponse, error)
	Delete(context.Context, *DeleteRequest) (*DeleteResponse, error)
	Walk(*WalkRequest, Store_WalkServer) error
}

func RegisterStoreServer(s *grpc.Server, srv StoreServer) {
	s.RegisterService(&_Store_serviceDesc, srv)
}

func _Store_Put_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PutRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StoreServer).Put(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.Store/Put",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StoreServer).Put(ctx, req.(*PutRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Store_Get_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StoreServer).Get(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.Store/Get",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StoreServer).Get(ctx, req.(*GetRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Store_Delete_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StoreServer).Delete(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.Store/Delete",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StoreServer).Delete(ctx, req.(*DeleteRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Store_Walk_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(WalkRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(StoreServer).Walk(m, &storeWalkServer{stream})
}

type Store_WalkServer interface {
	Send(*WalkResponse) error
	grpc.ServerStream
}

type storeWalkServer struct {
	grpc.ServerStream
}

func (x *storeWalkServer) Send(m *WalkResponse) error {
	return x.ServerStream.SendMsg(m)
}

var _Store_serviceDesc = grpc.ServiceDesc{
	ServiceName: "proto.Store",
	HandlerType: (*StoreServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Put",
			Handler:    _Store_Put_Handler,
		},
		{
			MethodName: "Get",
			Handler:    _Store_Get_Handler,
		},
		{
			MethodName: "Delete",
			Handler:    _Store_Delete_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Walk",
			Handler:       _Store_Walk_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "api.proto",
}

func init() { proto1.RegisterFile("api.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 313 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x8c, 0x51, 0x4d, 0x4b, 0xc3, 0x40,
	0x14, 0x6c, 0xfa, 0x11, 0xec, 0xa4, 0xad, 0xba, 0x55, 0x28, 0xc1, 0x43, 0x59, 0x10, 0x8a, 0x42,
	0xd1, 0x56, 0xf0, 0x0f, 0x08, 0xf5, 0x66, 0x59, 0x0b, 0x9e, 0x13, 0xfb, 0x82, 0x1f, 0xc1, 0x8d,
	0xc9, 0xe6, 0xe0, 0x0f, 0xf5, 0xff, 0x48, 0x76, 0x37, 0x5f, 0x1e, 0xc4, 0x53, 0xc2, 0xbc, 0x99,
	0x79, 0xf3, 0x66, 0x31, 0x0c, 0x92, 0xd7, 0x65, 0x92, 0x4a, 0x25, 0xd9, 0x40, 0x7f, 0xfc, 0x91,
	0x0c, 0xdf, 0xe8, 0x59, 0x19, 0xd0, 0x1f, 0xa6, 0x14, 0x99, 0x5f, 0xbe, 0x06, 0xb6, 0xb9, 0x12,
	0xf4, 0x99, 0x53, 0xa6, 0xd8, 0x39, 0x5c, 0x43, 0x9c, 0x39, 0x73, 0x67, 0xe1, 0xad, 0xc6, 0x86,
	0xb5, 0x7c, 0xd0, 0xa0, 0xb0, 0x43, 0x3e, 0x86, 0xa7, 0x45, 0x59, 0x22, 0x3f, 0x32, 0xe2, 0x17,
	0xc0, 0x86, 0x2a, 0x8f, 0x33, 0xf4, 0x52, 0x8a, 0xac, 0x01, 0xac, 0x81, 0xa0, 0x48, 0x14, 0x30,
	0xbf, 0x81, 0xa7, 0xb9, 0x46, 0xfa, 0xdf, 0x85, 0x87, 0x18, 0xdf, 0x51, 0x4c, 0x8a, 0xec, 0x12,
	0x7e, 0x84, 0x49, 0x09, 0xd8, 0x10, 0x3b, 0x78, 0x4f, 0x41, 0xfc, 0x5e, 0xa6, 0x60, 0xe8, 0xc7,
	0x32, 0xd8, 0x6b, 0xdb, 0x03, 0xa1, 0xff, 0xd9, 0x35, 0x60, 0xfc, 0x76, 0x5f, 0x09, 0xcd, 0xba,
	0x73, 0x67, 0x31, 0x59, 0x1d, 0xb7, 0x16, 0x16, 0x03, 0xd1, 0x20, 0xf1, 0x10, 0x23, 0xe3, 0x6a,
	0xf3, 0x5e, 0xc2, 0x7d, 0xa1, 0x60, 0x4f, 0xa9, 0xcd, 0x3b, 0x6d, 0xc9, 0xef, 0xf5, 0x48, 0x58,
	0x4a, 0xe3, 0xb8, 0xee, 0x1f, 0xc7, 0xad, 0xbe, 0x1d, 0x0c, 0x1e, 0x95, 0x4c, 0x89, 0x2d, 0xd1,
	0xdb, 0xe6, 0x8a, 0x95, 0x99, 0xea, 0x87, 0xf1, 0x59, 0x13, 0xb2, 0x17, 0x77, 0x0a, 0xfe, 0x86,
	0x6a, 0x7e, 0xfd, 0x08, 0x15, 0xbf, 0xd1, 0x35, 0xef, 0xb0, 0x5b, 0xb8, 0xa6, 0x35, 0x76, 0x62,
	0xe7, 0xad, 0x56, 0xfd, 0xd3, 0x5f, 0x68, 0x25, 0x5c, 0xa3, 0x5f, 0xd4, 0xc0, 0x4a, 0xdb, 0x46,
	0xd3, 0xfe, 0xb4, 0x85, 0x95, 0x92, 0x2b, 0x27, 0x74, 0x35, 0xbe, 0xfe, 0x09, 0x00, 0x00, 0xff,
	0xff, 0x3f, 0x37, 0x05, 0xf4, 0x8e, 0x02, 0x00, 0x00,
}
