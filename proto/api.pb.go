// Code generated by protoc-gen-go.
// source: api.proto
// DO NOT EDIT!

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
	ListResponse
	ListRequest
	Blob
	Commit
	FileInfo
	File
	FilePart
	Location
	Index
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

type PutRequest struct {
}

func (m *PutRequest) Reset()                    { *m = PutRequest{} }
func (m *PutRequest) String() string            { return proto1.CompactTextString(m) }
func (*PutRequest) ProtoMessage()               {}
func (*PutRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

type PutResponse struct {
}

func (m *PutResponse) Reset()                    { *m = PutResponse{} }
func (m *PutResponse) String() string            { return proto1.CompactTextString(m) }
func (*PutResponse) ProtoMessage()               {}
func (*PutResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

type GetRequest struct {
}

func (m *GetRequest) Reset()                    { *m = GetRequest{} }
func (m *GetRequest) String() string            { return proto1.CompactTextString(m) }
func (*GetRequest) ProtoMessage()               {}
func (*GetRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

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

type ListResponse struct {
}

func (m *ListResponse) Reset()                    { *m = ListResponse{} }
func (m *ListResponse) String() string            { return proto1.CompactTextString(m) }
func (*ListResponse) ProtoMessage()               {}
func (*ListResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{6} }

type ListRequest struct {
}

func (m *ListRequest) Reset()                    { *m = ListRequest{} }
func (m *ListRequest) String() string            { return proto1.CompactTextString(m) }
func (*ListRequest) ProtoMessage()               {}
func (*ListRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{7} }

func init() {
	proto1.RegisterType((*PutRequest)(nil), "proto.PutRequest")
	proto1.RegisterType((*PutResponse)(nil), "proto.PutResponse")
	proto1.RegisterType((*GetRequest)(nil), "proto.GetRequest")
	proto1.RegisterType((*GetResponse)(nil), "proto.GetResponse")
	proto1.RegisterType((*DeleteRequest)(nil), "proto.DeleteRequest")
	proto1.RegisterType((*DeleteResponse)(nil), "proto.DeleteResponse")
	proto1.RegisterType((*ListResponse)(nil), "proto.ListResponse")
	proto1.RegisterType((*ListRequest)(nil), "proto.ListRequest")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion3

// Client API for Store service

type StoreClient interface {
	Put(ctx context.Context, in *PutRequest, opts ...grpc.CallOption) (*PutResponse, error)
	Get(ctx context.Context, in *GetRequest, opts ...grpc.CallOption) (*GetResponse, error)
	Delete(ctx context.Context, in *DeleteRequest, opts ...grpc.CallOption) (*DeleteResponse, error)
	List(ctx context.Context, in *ListRequest, opts ...grpc.CallOption) (Store_ListClient, error)
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

func (c *storeClient) List(ctx context.Context, in *ListRequest, opts ...grpc.CallOption) (Store_ListClient, error) {
	stream, err := grpc.NewClientStream(ctx, &_Store_serviceDesc.Streams[0], c.cc, "/proto.Store/List", opts...)
	if err != nil {
		return nil, err
	}
	x := &storeListClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Store_ListClient interface {
	Recv() (*ListResponse, error)
	grpc.ClientStream
}

type storeListClient struct {
	grpc.ClientStream
}

func (x *storeListClient) Recv() (*ListResponse, error) {
	m := new(ListResponse)
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
	List(*ListRequest, Store_ListServer) error
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

func _Store_List_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(ListRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(StoreServer).List(m, &storeListServer{stream})
}

type Store_ListServer interface {
	Send(*ListResponse) error
	grpc.ServerStream
}

type storeListServer struct {
	grpc.ServerStream
}

func (x *storeListServer) Send(m *ListResponse) error {
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
			StreamName:    "List",
			Handler:       _Store_List_Handler,
			ServerStreams: true,
		},
	},
	Metadata: fileDescriptor0,
}

func init() { proto1.RegisterFile("api.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 236 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0xe2, 0xe2, 0x4c, 0x2c, 0xc8, 0xd4,
	0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0x62, 0x05, 0x53, 0x52, 0x3c, 0xf9, 0x49, 0x59, 0xa9, 0xc9,
	0x25, 0x10, 0x41, 0x29, 0xce, 0xa2, 0xd4, 0x34, 0x08, 0x53, 0x89, 0x87, 0x8b, 0x2b, 0xa0, 0xb4,
	0x24, 0x28, 0xb5, 0xb0, 0x34, 0xb5, 0xb8, 0x44, 0x89, 0x97, 0x8b, 0x1b, 0xcc, 0x2b, 0x2e, 0xc8,
	0xcf, 0x2b, 0x4e, 0x05, 0x49, 0xba, 0xa7, 0xc2, 0x25, 0x4d, 0xb8, 0xb8, 0xc1, 0x3c, 0x88, 0xa4,
	0x90, 0x2a, 0x17, 0x1b, 0xc4, 0x50, 0x09, 0x46, 0x05, 0x46, 0x0d, 0x6e, 0x23, 0x5e, 0x88, 0x89,
	0x7a, 0xfe, 0x60, 0xc1, 0x20, 0xa8, 0xa4, 0x12, 0x3f, 0x17, 0xaf, 0x4b, 0x6a, 0x4e, 0x6a, 0x49,
	0x2a, 0xcc, 0x18, 0x01, 0x2e, 0x3e, 0x98, 0x00, 0xd4, 0x1a, 0x3e, 0x2e, 0x1e, 0x9f, 0xcc, 0x62,
	0x84, 0xb5, 0x40, 0x57, 0x40, 0xf8, 0x60, 0x0d, 0x46, 0x57, 0x19, 0xb9, 0x58, 0x83, 0x4b, 0xf2,
	0x8b, 0x52, 0x85, 0xf4, 0xb8, 0x98, 0x81, 0xce, 0x13, 0x12, 0x84, 0xda, 0x84, 0x70, 0xb8, 0x94,
	0x10, 0xb2, 0x10, 0xd4, 0x18, 0x06, 0x90, 0x7a, 0xa0, 0x8b, 0xe1, 0xea, 0x11, 0x7e, 0x81, 0xab,
	0x47, 0xf2, 0x10, 0x50, 0xbd, 0x39, 0x17, 0x1b, 0xc4, 0x69, 0x42, 0x22, 0x50, 0x79, 0x14, 0xa7,
	0x4b, 0x89, 0xa2, 0x89, 0xc2, 0x35, 0x1a, 0x73, 0xb1, 0x80, 0x5c, 0x2c, 0x04, 0x33, 0x16, 0xc9,
	0xf9, 0x52, 0xc2, 0x28, 0x62, 0x30, 0x2d, 0x06, 0x8c, 0x49, 0x6c, 0x60, 0x71, 0x63, 0x40, 0x00,
	0x00, 0x00, 0xff, 0xff, 0x3e, 0x0b, 0x7c, 0x0d, 0xae, 0x01, 0x00, 0x00,
}
