package storage

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
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

var _ backup.Index = (*RemoteClient)(nil)

type RemoteClient struct {
	store proto.StoreClient
}

func (r *RemoteClient) Open() error  { return nil }
func (r *RemoteClient) Close() error { return nil }

func (r *RemoteClient) FileInfo(ctx context.Context, set string, name string, notAfter time.Time, count int) ([]*proto.TreeNode, error) {
	na := timestamppb.New(notAfter)

	response, err := r.store.FileInfo(ctx, &proto.FileInfoRequest{
		BackupSet: set,
		Path:      name,
		NotAfter:  na,
		Count:     int32(count),
	})

	if err != nil {
		return nil, err
	}

	return response.Files, nil
}

func (r *RemoteClient) CommitInfo(ctx context.Context, set string, notAfter time.Time, count int) ([]*proto.Commit, error) {
	na := timestamppb.New(notAfter)

	response, err := r.store.CommitInfo(ctx, &proto.CommitInfoRequest{
		BackupSet: set,
		NotAfter:  na,
		Count:     int32(count),
	})

	if err != nil {
		return nil, err
	}

	return response.Commits, nil
}

func (r *RemoteClient) ReIndex(ctx context.Context) error {
	return errors.New("not supported")
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

func NewRemoteServer(index backup.Index) *RemoteServer {
	return &RemoteServer{
		index: index,
	}
}

var _ proto.StoreServer = (*RemoteServer)(nil)

type RemoteServer struct {
	index backup.Index
}

func (r *RemoteServer) FileInfo(ctx context.Context, request *proto.FileInfoRequest) (*proto.FileInfoResponse, error) {
	err := request.NotAfter.CheckValid()
	if err != nil {
		return nil, err
	}

	notAfter := request.NotAfter.AsTime()

	files, err := r.index.FileInfo(ctx, request.BackupSet, request.Path, notAfter, int(request.Count))
	if err != nil {
		return nil, err
	}

	return &proto.FileInfoResponse{
		Files: files,
	}, nil
}

func (r *RemoteServer) CommitInfo(ctx context.Context, request *proto.CommitInfoRequest) (*proto.CommitInfoResponse, error) {
	err := request.NotAfter.CheckValid()
	if err != nil {
		return nil, err
	}

	notAfter := request.NotAfter.AsTime()

	commits, err := r.index.CommitInfo(ctx, request.BackupSet, notAfter, int(request.Count))
	if err != nil {
		return nil, err
	}

	return &proto.CommitInfoResponse{
		Commits: commits,
	}, nil
}

func (r *RemoteServer) Put(ctx context.Context, request *proto.PutRequest) (*proto.PutResponse, error) {
	return &proto.PutResponse{}, r.index.Put(ctx, request.Object)
}

func (r *RemoteServer) Get(ctx context.Context, request *proto.GetRequest) (*proto.GetResponse, error) {
	obj, err := r.index.Get(ctx, request.Ref)

	return &proto.GetResponse{Object: obj}, err
}

func (r *RemoteServer) Delete(ctx context.Context, request *proto.DeleteRequest) (*proto.DeleteResponse, error) {
	err := r.index.Delete(ctx, request.Ref)
	if err != nil {
		return nil, err
	}
	return &proto.DeleteResponse{}, nil
}

func (r *RemoteServer) Walk(request *proto.WalkRequest, walker proto.Store_WalkServer) error {
	return r.index.Walk(walker.Context(), request.Load, request.ObjectType, func(header *proto.ObjectHeader, object *proto.Object) error {
		return walker.Send(&proto.WalkResponse{Object: object, Header: header})
	})
}

func (r *RemoteServer) Has(ctx context.Context, request *proto.HasRequest) (*proto.HasResponse, error) {
	has, err := r.index.Has(ctx, request.Ref)

	if err != nil {
		return nil, err
	}

	return &proto.HasResponse{Has: has}, nil
}
