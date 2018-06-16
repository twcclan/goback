package storage

import (
	"context"
	"io"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
	"google.golang.org/grpc"
)

func NewRemoteClient(addr string) (*RemoteClient, error) {
	con, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	return &RemoteClient{
		store: proto.NewStoreClient(con),
	}, nil
}

type RemoteClient struct {
	store proto.StoreClient
}

func (r *RemoteClient) Put(ctx context.Context, object *proto.Object) error {
	_, err := r.store.Put(ctx, &proto.PutRequest{Object: object})

	return err
}

func (r *RemoteClient) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	resp, err := r.store.Get(ctx, &proto.GetRequest{Ref: ref})
	if err != nil {
		return nil, err
	}

	return resp.Object, nil
}

func (r *RemoteClient) Delete(ctx context.Context, ref *proto.Ref) error {
	_, err := r.store.Delete(ctx, &proto.DeleteRequest{Ref: ref})

	return err
}

func (r *RemoteClient) Walk(ctx context.Context, load bool, typ proto.ObjectType, fn backup.ObjectReceiver) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	walker, err := r.store.Walk(ctx, &proto.WalkRequest{Load: load, ObjectType: typ})
	if err != nil {
		return err
	}

	defer walker.CloseSend()

	for {
		resp, err := walker.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		err = fn(resp.Header, resp.Object)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *RemoteClient) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	response, err := r.store.Has(ctx, &proto.HasRequest{Ref: ref})
	if err != nil {
		return false, err
	}

	return response.Has, nil
}

func NewRemoteServer(store backup.ObjectStore) *RemoteServer {
	return &RemoteServer{
		store: store,
	}
}

type RemoteServer struct {
	store backup.ObjectStore
}

func (r *RemoteServer) Put(ctx context.Context, request *proto.PutRequest) (*proto.PutResponse, error) {
	return &proto.PutResponse{}, r.store.Put(ctx, request.Object)
}

func (r *RemoteServer) Get(ctx context.Context, request *proto.GetRequest) (*proto.GetResponse, error) {
	obj, err := r.store.Get(ctx, request.Ref)

	return &proto.GetResponse{Object: obj}, err
}

func (r *RemoteServer) Delete(ctx context.Context, request *proto.DeleteRequest) (*proto.DeleteResponse, error) {
	err := r.store.Delete(ctx, request.Ref)
	if err != nil {
		return nil, err
	}
	return &proto.DeleteResponse{}, nil
}

func (r *RemoteServer) Walk(request *proto.WalkRequest, walker proto.Store_WalkServer) error {
	return r.store.Walk(walker.Context(), request.Load, request.ObjectType, func(header *proto.ObjectHeader, object *proto.Object) error {
		return walker.Send(&proto.WalkResponse{Object: object, Header: header})
	})
}

func (r *RemoteServer) Has(ctx context.Context, request *proto.HasRequest) (*proto.HasResponse, error) {
	has, err := r.store.Has(ctx, request.Ref)

	if err != nil {
		return nil, err
	}

	return &proto.HasResponse{Has: has}, nil
}
