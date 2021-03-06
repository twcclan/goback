// Code generated by protoc-gen-go. DO NOT EDIT.
// source: api.proto

package proto

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import timestamp "github.com/golang/protobuf/ptypes/timestamp"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

// Put(*proto.Object) error
type PutRequest struct {
	Object               *Object  `protobuf:"bytes,1,opt,name=object,proto3" json:"object,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *PutRequest) Reset()         { *m = PutRequest{} }
func (m *PutRequest) String() string { return proto.CompactTextString(m) }
func (*PutRequest) ProtoMessage()    {}
func (*PutRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{0}
}
func (m *PutRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_PutRequest.Unmarshal(m, b)
}
func (m *PutRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_PutRequest.Marshal(b, m, deterministic)
}
func (dst *PutRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_PutRequest.Merge(dst, src)
}
func (m *PutRequest) XXX_Size() int {
	return xxx_messageInfo_PutRequest.Size(m)
}
func (m *PutRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_PutRequest.DiscardUnknown(m)
}

var xxx_messageInfo_PutRequest proto.InternalMessageInfo

func (m *PutRequest) GetObject() *Object {
	if m != nil {
		return m.Object
	}
	return nil
}

type PutResponse struct {
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *PutResponse) Reset()         { *m = PutResponse{} }
func (m *PutResponse) String() string { return proto.CompactTextString(m) }
func (*PutResponse) ProtoMessage()    {}
func (*PutResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{1}
}
func (m *PutResponse) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_PutResponse.Unmarshal(m, b)
}
func (m *PutResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_PutResponse.Marshal(b, m, deterministic)
}
func (dst *PutResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_PutResponse.Merge(dst, src)
}
func (m *PutResponse) XXX_Size() int {
	return xxx_messageInfo_PutResponse.Size(m)
}
func (m *PutResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_PutResponse.DiscardUnknown(m)
}

var xxx_messageInfo_PutResponse proto.InternalMessageInfo

// Get(*proto.Ref) (*proto.Object, error)
type GetRequest struct {
	Ref                  *Ref     `protobuf:"bytes,1,opt,name=ref,proto3" json:"ref,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *GetRequest) Reset()         { *m = GetRequest{} }
func (m *GetRequest) String() string { return proto.CompactTextString(m) }
func (*GetRequest) ProtoMessage()    {}
func (*GetRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{2}
}
func (m *GetRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_GetRequest.Unmarshal(m, b)
}
func (m *GetRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_GetRequest.Marshal(b, m, deterministic)
}
func (dst *GetRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_GetRequest.Merge(dst, src)
}
func (m *GetRequest) XXX_Size() int {
	return xxx_messageInfo_GetRequest.Size(m)
}
func (m *GetRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_GetRequest.DiscardUnknown(m)
}

var xxx_messageInfo_GetRequest proto.InternalMessageInfo

func (m *GetRequest) GetRef() *Ref {
	if m != nil {
		return m.Ref
	}
	return nil
}

type GetResponse struct {
	Object               *Object  `protobuf:"bytes,1,opt,name=object,proto3" json:"object,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *GetResponse) Reset()         { *m = GetResponse{} }
func (m *GetResponse) String() string { return proto.CompactTextString(m) }
func (*GetResponse) ProtoMessage()    {}
func (*GetResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{3}
}
func (m *GetResponse) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_GetResponse.Unmarshal(m, b)
}
func (m *GetResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_GetResponse.Marshal(b, m, deterministic)
}
func (dst *GetResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_GetResponse.Merge(dst, src)
}
func (m *GetResponse) XXX_Size() int {
	return xxx_messageInfo_GetResponse.Size(m)
}
func (m *GetResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_GetResponse.DiscardUnknown(m)
}

var xxx_messageInfo_GetResponse proto.InternalMessageInfo

func (m *GetResponse) GetObject() *Object {
	if m != nil {
		return m.Object
	}
	return nil
}

// Delete(*proto.Ref) error
type DeleteRequest struct {
	Ref                  *Ref     `protobuf:"bytes,1,opt,name=ref,proto3" json:"ref,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *DeleteRequest) Reset()         { *m = DeleteRequest{} }
func (m *DeleteRequest) String() string { return proto.CompactTextString(m) }
func (*DeleteRequest) ProtoMessage()    {}
func (*DeleteRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{4}
}
func (m *DeleteRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_DeleteRequest.Unmarshal(m, b)
}
func (m *DeleteRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_DeleteRequest.Marshal(b, m, deterministic)
}
func (dst *DeleteRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_DeleteRequest.Merge(dst, src)
}
func (m *DeleteRequest) XXX_Size() int {
	return xxx_messageInfo_DeleteRequest.Size(m)
}
func (m *DeleteRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_DeleteRequest.DiscardUnknown(m)
}

var xxx_messageInfo_DeleteRequest proto.InternalMessageInfo

func (m *DeleteRequest) GetRef() *Ref {
	if m != nil {
		return m.Ref
	}
	return nil
}

type DeleteResponse struct {
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *DeleteResponse) Reset()         { *m = DeleteResponse{} }
func (m *DeleteResponse) String() string { return proto.CompactTextString(m) }
func (*DeleteResponse) ProtoMessage()    {}
func (*DeleteResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{5}
}
func (m *DeleteResponse) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_DeleteResponse.Unmarshal(m, b)
}
func (m *DeleteResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_DeleteResponse.Marshal(b, m, deterministic)
}
func (dst *DeleteResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_DeleteResponse.Merge(dst, src)
}
func (m *DeleteResponse) XXX_Size() int {
	return xxx_messageInfo_DeleteResponse.Size(m)
}
func (m *DeleteResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_DeleteResponse.DiscardUnknown(m)
}

var xxx_messageInfo_DeleteResponse proto.InternalMessageInfo

// Walk(bool, proto.ObjectType, ObjectReceiver) error
type WalkRequest struct {
	Load                 bool       `protobuf:"varint,1,opt,name=load,proto3" json:"load,omitempty"`
	ObjectType           ObjectType `protobuf:"varint,2,opt,name=objectType,proto3,enum=proto.ObjectType" json:"objectType,omitempty"`
	XXX_NoUnkeyedLiteral struct{}   `json:"-"`
	XXX_unrecognized     []byte     `json:"-"`
	XXX_sizecache        int32      `json:"-"`
}

func (m *WalkRequest) Reset()         { *m = WalkRequest{} }
func (m *WalkRequest) String() string { return proto.CompactTextString(m) }
func (*WalkRequest) ProtoMessage()    {}
func (*WalkRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{6}
}
func (m *WalkRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_WalkRequest.Unmarshal(m, b)
}
func (m *WalkRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_WalkRequest.Marshal(b, m, deterministic)
}
func (dst *WalkRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_WalkRequest.Merge(dst, src)
}
func (m *WalkRequest) XXX_Size() int {
	return xxx_messageInfo_WalkRequest.Size(m)
}
func (m *WalkRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_WalkRequest.DiscardUnknown(m)
}

var xxx_messageInfo_WalkRequest proto.InternalMessageInfo

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
	Header               *ObjectHeader `protobuf:"bytes,1,opt,name=header,proto3" json:"header,omitempty"`
	Object               *Object       `protobuf:"bytes,2,opt,name=object,proto3" json:"object,omitempty"`
	XXX_NoUnkeyedLiteral struct{}      `json:"-"`
	XXX_unrecognized     []byte        `json:"-"`
	XXX_sizecache        int32         `json:"-"`
}

func (m *WalkResponse) Reset()         { *m = WalkResponse{} }
func (m *WalkResponse) String() string { return proto.CompactTextString(m) }
func (*WalkResponse) ProtoMessage()    {}
func (*WalkResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{7}
}
func (m *WalkResponse) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_WalkResponse.Unmarshal(m, b)
}
func (m *WalkResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_WalkResponse.Marshal(b, m, deterministic)
}
func (dst *WalkResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_WalkResponse.Merge(dst, src)
}
func (m *WalkResponse) XXX_Size() int {
	return xxx_messageInfo_WalkResponse.Size(m)
}
func (m *WalkResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_WalkResponse.DiscardUnknown(m)
}

var xxx_messageInfo_WalkResponse proto.InternalMessageInfo

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

// Has(context.Context, *proto.Ref) (bool, error)
type HasRequest struct {
	Ref                  *Ref     `protobuf:"bytes,1,opt,name=ref,proto3" json:"ref,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *HasRequest) Reset()         { *m = HasRequest{} }
func (m *HasRequest) String() string { return proto.CompactTextString(m) }
func (*HasRequest) ProtoMessage()    {}
func (*HasRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{8}
}
func (m *HasRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_HasRequest.Unmarshal(m, b)
}
func (m *HasRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_HasRequest.Marshal(b, m, deterministic)
}
func (dst *HasRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_HasRequest.Merge(dst, src)
}
func (m *HasRequest) XXX_Size() int {
	return xxx_messageInfo_HasRequest.Size(m)
}
func (m *HasRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_HasRequest.DiscardUnknown(m)
}

var xxx_messageInfo_HasRequest proto.InternalMessageInfo

func (m *HasRequest) GetRef() *Ref {
	if m != nil {
		return m.Ref
	}
	return nil
}

type HasResponse struct {
	Has                  bool     `protobuf:"varint,1,opt,name=has,proto3" json:"has,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *HasResponse) Reset()         { *m = HasResponse{} }
func (m *HasResponse) String() string { return proto.CompactTextString(m) }
func (*HasResponse) ProtoMessage()    {}
func (*HasResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{9}
}
func (m *HasResponse) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_HasResponse.Unmarshal(m, b)
}
func (m *HasResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_HasResponse.Marshal(b, m, deterministic)
}
func (dst *HasResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_HasResponse.Merge(dst, src)
}
func (m *HasResponse) XXX_Size() int {
	return xxx_messageInfo_HasResponse.Size(m)
}
func (m *HasResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_HasResponse.DiscardUnknown(m)
}

var xxx_messageInfo_HasResponse proto.InternalMessageInfo

func (m *HasResponse) GetHas() bool {
	if m != nil {
		return m.Has
	}
	return false
}

type FileInfoRequest struct {
	BackupSet            string               `protobuf:"bytes,1,opt,name=backup_set,json=backupSet,proto3" json:"backup_set,omitempty"`
	Path                 string               `protobuf:"bytes,2,opt,name=path,proto3" json:"path,omitempty"`
	NotAfter             *timestamp.Timestamp `protobuf:"bytes,3,opt,name=notAfter,proto3" json:"notAfter,omitempty"`
	Count                int32                `protobuf:"varint,4,opt,name=count,proto3" json:"count,omitempty"`
	XXX_NoUnkeyedLiteral struct{}             `json:"-"`
	XXX_unrecognized     []byte               `json:"-"`
	XXX_sizecache        int32                `json:"-"`
}

func (m *FileInfoRequest) Reset()         { *m = FileInfoRequest{} }
func (m *FileInfoRequest) String() string { return proto.CompactTextString(m) }
func (*FileInfoRequest) ProtoMessage()    {}
func (*FileInfoRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{10}
}
func (m *FileInfoRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_FileInfoRequest.Unmarshal(m, b)
}
func (m *FileInfoRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_FileInfoRequest.Marshal(b, m, deterministic)
}
func (dst *FileInfoRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_FileInfoRequest.Merge(dst, src)
}
func (m *FileInfoRequest) XXX_Size() int {
	return xxx_messageInfo_FileInfoRequest.Size(m)
}
func (m *FileInfoRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_FileInfoRequest.DiscardUnknown(m)
}

var xxx_messageInfo_FileInfoRequest proto.InternalMessageInfo

func (m *FileInfoRequest) GetBackupSet() string {
	if m != nil {
		return m.BackupSet
	}
	return ""
}

func (m *FileInfoRequest) GetPath() string {
	if m != nil {
		return m.Path
	}
	return ""
}

func (m *FileInfoRequest) GetNotAfter() *timestamp.Timestamp {
	if m != nil {
		return m.NotAfter
	}
	return nil
}

func (m *FileInfoRequest) GetCount() int32 {
	if m != nil {
		return m.Count
	}
	return 0
}

type FileInfoResponse struct {
	Files                []*TreeNode `protobuf:"bytes,1,rep,name=files,proto3" json:"files,omitempty"`
	XXX_NoUnkeyedLiteral struct{}    `json:"-"`
	XXX_unrecognized     []byte      `json:"-"`
	XXX_sizecache        int32       `json:"-"`
}

func (m *FileInfoResponse) Reset()         { *m = FileInfoResponse{} }
func (m *FileInfoResponse) String() string { return proto.CompactTextString(m) }
func (*FileInfoResponse) ProtoMessage()    {}
func (*FileInfoResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{11}
}
func (m *FileInfoResponse) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_FileInfoResponse.Unmarshal(m, b)
}
func (m *FileInfoResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_FileInfoResponse.Marshal(b, m, deterministic)
}
func (dst *FileInfoResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_FileInfoResponse.Merge(dst, src)
}
func (m *FileInfoResponse) XXX_Size() int {
	return xxx_messageInfo_FileInfoResponse.Size(m)
}
func (m *FileInfoResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_FileInfoResponse.DiscardUnknown(m)
}

var xxx_messageInfo_FileInfoResponse proto.InternalMessageInfo

func (m *FileInfoResponse) GetFiles() []*TreeNode {
	if m != nil {
		return m.Files
	}
	return nil
}

type CommitInfoRequest struct {
	BackupSet            string               `protobuf:"bytes,1,opt,name=backup_set,json=backupSet,proto3" json:"backup_set,omitempty"`
	NotAfter             *timestamp.Timestamp `protobuf:"bytes,2,opt,name=notAfter,proto3" json:"notAfter,omitempty"`
	Count                int32                `protobuf:"varint,3,opt,name=count,proto3" json:"count,omitempty"`
	XXX_NoUnkeyedLiteral struct{}             `json:"-"`
	XXX_unrecognized     []byte               `json:"-"`
	XXX_sizecache        int32                `json:"-"`
}

func (m *CommitInfoRequest) Reset()         { *m = CommitInfoRequest{} }
func (m *CommitInfoRequest) String() string { return proto.CompactTextString(m) }
func (*CommitInfoRequest) ProtoMessage()    {}
func (*CommitInfoRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{12}
}
func (m *CommitInfoRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_CommitInfoRequest.Unmarshal(m, b)
}
func (m *CommitInfoRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_CommitInfoRequest.Marshal(b, m, deterministic)
}
func (dst *CommitInfoRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_CommitInfoRequest.Merge(dst, src)
}
func (m *CommitInfoRequest) XXX_Size() int {
	return xxx_messageInfo_CommitInfoRequest.Size(m)
}
func (m *CommitInfoRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_CommitInfoRequest.DiscardUnknown(m)
}

var xxx_messageInfo_CommitInfoRequest proto.InternalMessageInfo

func (m *CommitInfoRequest) GetBackupSet() string {
	if m != nil {
		return m.BackupSet
	}
	return ""
}

func (m *CommitInfoRequest) GetNotAfter() *timestamp.Timestamp {
	if m != nil {
		return m.NotAfter
	}
	return nil
}

func (m *CommitInfoRequest) GetCount() int32 {
	if m != nil {
		return m.Count
	}
	return 0
}

type CommitInfoResponse struct {
	Commits              []*Commit `protobuf:"bytes,1,rep,name=commits,proto3" json:"commits,omitempty"`
	XXX_NoUnkeyedLiteral struct{}  `json:"-"`
	XXX_unrecognized     []byte    `json:"-"`
	XXX_sizecache        int32     `json:"-"`
}

func (m *CommitInfoResponse) Reset()         { *m = CommitInfoResponse{} }
func (m *CommitInfoResponse) String() string { return proto.CompactTextString(m) }
func (*CommitInfoResponse) ProtoMessage()    {}
func (*CommitInfoResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_api_c673c522061b2398, []int{13}
}
func (m *CommitInfoResponse) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_CommitInfoResponse.Unmarshal(m, b)
}
func (m *CommitInfoResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_CommitInfoResponse.Marshal(b, m, deterministic)
}
func (dst *CommitInfoResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_CommitInfoResponse.Merge(dst, src)
}
func (m *CommitInfoResponse) XXX_Size() int {
	return xxx_messageInfo_CommitInfoResponse.Size(m)
}
func (m *CommitInfoResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_CommitInfoResponse.DiscardUnknown(m)
}

var xxx_messageInfo_CommitInfoResponse proto.InternalMessageInfo

func (m *CommitInfoResponse) GetCommits() []*Commit {
	if m != nil {
		return m.Commits
	}
	return nil
}

func init() {
	proto.RegisterType((*PutRequest)(nil), "proto.PutRequest")
	proto.RegisterType((*PutResponse)(nil), "proto.PutResponse")
	proto.RegisterType((*GetRequest)(nil), "proto.GetRequest")
	proto.RegisterType((*GetResponse)(nil), "proto.GetResponse")
	proto.RegisterType((*DeleteRequest)(nil), "proto.DeleteRequest")
	proto.RegisterType((*DeleteResponse)(nil), "proto.DeleteResponse")
	proto.RegisterType((*WalkRequest)(nil), "proto.WalkRequest")
	proto.RegisterType((*WalkResponse)(nil), "proto.WalkResponse")
	proto.RegisterType((*HasRequest)(nil), "proto.HasRequest")
	proto.RegisterType((*HasResponse)(nil), "proto.HasResponse")
	proto.RegisterType((*FileInfoRequest)(nil), "proto.FileInfoRequest")
	proto.RegisterType((*FileInfoResponse)(nil), "proto.FileInfoResponse")
	proto.RegisterType((*CommitInfoRequest)(nil), "proto.CommitInfoRequest")
	proto.RegisterType((*CommitInfoResponse)(nil), "proto.CommitInfoResponse")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// StoreClient is the client API for Store service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type StoreClient interface {
	Put(ctx context.Context, in *PutRequest, opts ...grpc.CallOption) (*PutResponse, error)
	Get(ctx context.Context, in *GetRequest, opts ...grpc.CallOption) (*GetResponse, error)
	Delete(ctx context.Context, in *DeleteRequest, opts ...grpc.CallOption) (*DeleteResponse, error)
	Walk(ctx context.Context, in *WalkRequest, opts ...grpc.CallOption) (Store_WalkClient, error)
	Has(ctx context.Context, in *HasRequest, opts ...grpc.CallOption) (*HasResponse, error)
	FileInfo(ctx context.Context, in *FileInfoRequest, opts ...grpc.CallOption) (*FileInfoResponse, error)
	CommitInfo(ctx context.Context, in *CommitInfoRequest, opts ...grpc.CallOption) (*CommitInfoResponse, error)
}

type storeClient struct {
	cc *grpc.ClientConn
}

func NewStoreClient(cc *grpc.ClientConn) StoreClient {
	return &storeClient{cc}
}

func (c *storeClient) Put(ctx context.Context, in *PutRequest, opts ...grpc.CallOption) (*PutResponse, error) {
	out := new(PutResponse)
	err := c.cc.Invoke(ctx, "/proto.Store/Put", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *storeClient) Get(ctx context.Context, in *GetRequest, opts ...grpc.CallOption) (*GetResponse, error) {
	out := new(GetResponse)
	err := c.cc.Invoke(ctx, "/proto.Store/Get", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *storeClient) Delete(ctx context.Context, in *DeleteRequest, opts ...grpc.CallOption) (*DeleteResponse, error) {
	out := new(DeleteResponse)
	err := c.cc.Invoke(ctx, "/proto.Store/Delete", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *storeClient) Walk(ctx context.Context, in *WalkRequest, opts ...grpc.CallOption) (Store_WalkClient, error) {
	stream, err := c.cc.NewStream(ctx, &_Store_serviceDesc.Streams[0], "/proto.Store/Walk", opts...)
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

func (c *storeClient) Has(ctx context.Context, in *HasRequest, opts ...grpc.CallOption) (*HasResponse, error) {
	out := new(HasResponse)
	err := c.cc.Invoke(ctx, "/proto.Store/Has", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *storeClient) FileInfo(ctx context.Context, in *FileInfoRequest, opts ...grpc.CallOption) (*FileInfoResponse, error) {
	out := new(FileInfoResponse)
	err := c.cc.Invoke(ctx, "/proto.Store/FileInfo", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *storeClient) CommitInfo(ctx context.Context, in *CommitInfoRequest, opts ...grpc.CallOption) (*CommitInfoResponse, error) {
	out := new(CommitInfoResponse)
	err := c.cc.Invoke(ctx, "/proto.Store/CommitInfo", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// StoreServer is the server API for Store service.
type StoreServer interface {
	Put(context.Context, *PutRequest) (*PutResponse, error)
	Get(context.Context, *GetRequest) (*GetResponse, error)
	Delete(context.Context, *DeleteRequest) (*DeleteResponse, error)
	Walk(*WalkRequest, Store_WalkServer) error
	Has(context.Context, *HasRequest) (*HasResponse, error)
	FileInfo(context.Context, *FileInfoRequest) (*FileInfoResponse, error)
	CommitInfo(context.Context, *CommitInfoRequest) (*CommitInfoResponse, error)
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

func _Store_Has_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HasRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StoreServer).Has(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.Store/Has",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StoreServer).Has(ctx, req.(*HasRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Store_FileInfo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(FileInfoRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StoreServer).FileInfo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.Store/FileInfo",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StoreServer).FileInfo(ctx, req.(*FileInfoRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Store_CommitInfo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CommitInfoRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(StoreServer).CommitInfo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.Store/CommitInfo",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(StoreServer).CommitInfo(ctx, req.(*CommitInfoRequest))
	}
	return interceptor(ctx, in, info, handler)
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
		{
			MethodName: "Has",
			Handler:    _Store_Has_Handler,
		},
		{
			MethodName: "FileInfo",
			Handler:    _Store_FileInfo_Handler,
		},
		{
			MethodName: "CommitInfo",
			Handler:    _Store_CommitInfo_Handler,
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

func init() { proto.RegisterFile("api.proto", fileDescriptor_api_c673c522061b2398) }

var fileDescriptor_api_c673c522061b2398 = []byte{
	// 570 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x9c, 0x53, 0xcf, 0x6f, 0xd3, 0x4c,
	0x10, 0x8d, 0xe3, 0x24, 0x5f, 0x32, 0x4e, 0xda, 0x74, 0xdb, 0x0f, 0x82, 0x05, 0x6a, 0x64, 0x09,
	0x11, 0x15, 0xe1, 0x42, 0x82, 0x40, 0x1c, 0x7a, 0x40, 0x45, 0x34, 0x5c, 0xa0, 0xda, 0x46, 0xe2,
	0x88, 0x9c, 0x64, 0xdc, 0x84, 0x3a, 0x59, 0x63, 0xaf, 0x0f, 0xdc, 0xb8, 0x72, 0xe5, 0x2f, 0x46,
	0xde, 0x1f, 0xf6, 0xba, 0x20, 0x14, 0x38, 0x79, 0x3c, 0xf3, 0x66, 0xf6, 0xbd, 0xdd, 0x37, 0xd0,
	0x09, 0xe2, 0xb5, 0x1f, 0x27, 0x8c, 0x33, 0xd2, 0x14, 0x1f, 0xb7, 0xcb, 0xe6, 0x9f, 0x71, 0xc1,
	0x65, 0xd2, 0xed, 0x24, 0x18, 0xaa, 0x10, 0x78, 0x82, 0xa8, 0xe2, 0xee, 0x82, 0x6d, 0x36, 0x6b,
	0x0d, 0x3a, 0xbe, 0x66, 0xec, 0x3a, 0xc2, 0x53, 0xf1, 0x37, 0xcf, 0xc2, 0x53, 0xbe, 0xde, 0x60,
	0xca, 0x83, 0x4d, 0x2c, 0x01, 0xde, 0x04, 0xe0, 0x32, 0xe3, 0x14, 0xbf, 0x64, 0x98, 0x72, 0xf2,
	0x10, 0x5a, 0xf2, 0x8c, 0x81, 0x35, 0xb4, 0x46, 0xce, 0xb8, 0x27, 0x51, 0xfe, 0x07, 0x91, 0xa4,
	0xaa, 0xe8, 0xf5, 0xc0, 0x11, 0x4d, 0x69, 0xcc, 0xb6, 0x29, 0x7a, 0x27, 0x00, 0x17, 0x58, 0xcc,
	0xb8, 0x0f, 0x76, 0x82, 0xa1, 0x1a, 0x00, 0x6a, 0x00, 0xc5, 0x90, 0xe6, 0x69, 0xef, 0x39, 0x38,
	0x02, 0x2b, 0x5b, 0x77, 0x3d, 0xf0, 0x09, 0xf4, 0xde, 0x60, 0x84, 0x1c, 0x77, 0x3b, 0xa4, 0x0f,
	0x7b, 0x1a, 0xae, 0x28, 0xce, 0xc0, 0xf9, 0x18, 0x44, 0x37, 0xba, 0x9d, 0x40, 0x23, 0x62, 0xc1,
	0x52, 0xf4, 0xb7, 0xa9, 0x88, 0xc9, 0x33, 0x00, 0x79, 0xda, 0xec, 0x6b, 0x8c, 0x83, 0xfa, 0xd0,
	0x1a, 0xed, 0x8d, 0x0f, 0x2a, 0x74, 0xf2, 0x02, 0x35, 0x40, 0xde, 0x1c, 0xba, 0x72, 0xaa, 0x52,
	0xf3, 0x18, 0x5a, 0x2b, 0x0c, 0x96, 0x98, 0x28, 0x62, 0x87, 0x95, 0xf6, 0xa9, 0x28, 0x51, 0x05,
	0x31, 0xa4, 0xd7, 0xff, 0x24, 0xfd, 0x04, 0x60, 0x1a, 0xa4, 0xbb, 0xe9, 0x3e, 0x06, 0x47, 0x60,
	0x15, 0x9d, 0x3e, 0xd8, 0xab, 0x20, 0x55, 0x22, 0xf3, 0xd0, 0xfb, 0x61, 0xc1, 0xfe, 0xdb, 0x75,
	0x84, 0xef, 0xb6, 0x21, 0xd3, 0x23, 0x1f, 0x00, 0xcc, 0x83, 0xc5, 0x4d, 0x16, 0x7f, 0x4a, 0x51,
	0x3e, 0x43, 0x87, 0x76, 0x64, 0xe6, 0x0a, 0xc5, 0x55, 0xc5, 0x01, 0x5f, 0x09, 0x92, 0x1d, 0x2a,
	0x62, 0xf2, 0x02, 0xda, 0x5b, 0xc6, 0x5f, 0x87, 0x1c, 0x93, 0x81, 0x2d, 0xa8, 0xb8, 0xbe, 0x34,
	0x9a, 0xaf, 0x8d, 0xe6, 0xcf, 0xb4, 0xd1, 0x68, 0x81, 0x25, 0x47, 0xd0, 0x5c, 0xb0, 0x6c, 0xcb,
	0x07, 0x8d, 0xa1, 0x35, 0x6a, 0x52, 0xf9, 0xe3, 0xbd, 0x82, 0x7e, 0xc9, 0xa9, 0xf0, 0x45, 0x33,
	0x5c, 0x47, 0x98, 0x93, 0xb7, 0x47, 0xce, 0x78, 0x5f, 0x29, 0x9d, 0x25, 0x88, 0xef, 0xd9, 0x12,
	0xa9, 0xac, 0x7a, 0xdf, 0x2c, 0x38, 0x38, 0x17, 0x7e, 0xff, 0x0b, 0x45, 0x26, 0xfb, 0xfa, 0xbf,
	0xb0, 0xb7, 0x4d, 0xf6, 0x67, 0x40, 0x4c, 0x06, 0x8a, 0xff, 0x23, 0xf8, 0x4f, 0xee, 0xa1, 0x56,
	0xa0, 0x5f, 0x57, 0x62, 0xa9, 0xae, 0x8e, 0xbf, 0xdb, 0xd0, 0xbc, 0xe2, 0x2c, 0x41, 0xe2, 0x83,
	0x7d, 0x99, 0x71, 0xa2, 0x2d, 0x57, 0x6e, 0xa5, 0x4b, 0xcc, 0x94, 0x32, 0x74, 0x2d, 0xc7, 0x5f,
	0x60, 0x89, 0x2f, 0x37, 0xb0, 0xc0, 0x1b, 0x8b, 0xe6, 0xd5, 0xc8, 0x4b, 0x68, 0xc9, 0xa5, 0x20,
	0x47, 0xaa, 0x5e, 0x59, 0x29, 0xf7, 0xff, 0x5b, 0xd9, 0xa2, 0x71, 0x02, 0x8d, 0xdc, 0xe5, 0x44,
	0x8f, 0x35, 0x16, 0xc9, 0x3d, 0xac, 0xe4, 0x74, 0xcb, 0x53, 0x2b, 0x67, 0x37, 0x0d, 0xd2, 0x82,
	0x5d, 0x69, 0xe1, 0x82, 0x9d, 0xe1, 0x54, 0xaf, 0x46, 0xce, 0xa0, 0xad, 0x4d, 0x40, 0xee, 0x28,
	0xc4, 0x2d, 0xa7, 0xba, 0x77, 0x7f, 0xc9, 0x17, 0xed, 0xe7, 0x00, 0xe5, 0x2b, 0x90, 0x41, 0xe5,
	0xb2, 0xcd, 0x11, 0xf7, 0x7e, 0x53, 0xd1, 0x43, 0xe6, 0x2d, 0x51, 0x9b, 0xfc, 0x0c, 0x00, 0x00,
	0xff, 0xff, 0x71, 0xfe, 0x14, 0x5a, 0x7a, 0x05, 0x00, 0x00,
}
