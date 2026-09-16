package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func (r *RemoteClient) ReachableCommits(ctx context.Context, f func(commit *proto.Commit) error) error {
	panic("implement me")
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

// LatestCommit implements backup.Index.
func (r *RemoteClient) LatestCommit(ctx context.Context, set string) (*proto.Ref, error) {
	response, err := r.store.LatestCommit(ctx, &proto.LatestCommitRequest{BackupSet: set})
	if err != nil {
		return nil, err
	}

	if response.Ref == nil {
		return nil, backup.ErrNotFound
	}

	return response.Ref, nil
}

// GetTree implements backup.TreeFetcher over the streaming RPC.
func (r *RemoteClient) GetTree(ctx context.Context, ref *proto.Ref, maxDepth uint32) ([]*proto.Object, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := r.store.GetTree(ctx, &proto.GetTreeRequest{Ref: ref, MaxDepth: maxDepth})
	if err != nil {
		return nil, err
	}

	var objects []*proto.Object
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			return objects, nil
		}

		if err != nil {
			if status.Code(err) == codes.NotFound {
				return nil, backup.ErrNotFound
			}

			return nil, err
		}

		objects = append(objects, resp.Object)
	}
}

func (r *RemoteClient) Put(ctx context.Context, object *proto.Object) error {
	payload, err := object.Canonical()
	if err != nil {
		return err
	}

	_, err = r.store.Put(ctx, &proto.PutRequest{Object: object, Ref: proto.HashPayload(object.Type(), payload)})
	if status.Code(err) == codes.FailedPrecondition {
		return fmt.Errorf("%w: %s", backup.ErrDanglingRef, status.Convert(err).Message())
	}

	return err
}

func (r *RemoteClient) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	resp, err := r.store.Get(ctx, &proto.GetRequest{Ref: ref})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, backup.ErrNotFound
		}

		return nil, err
	}

	if resp.Object == nil {
		return nil, backup.ErrNotFound
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

		err = fn(resp.Object)
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
	proto.UnsafeStoreServer
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

// Put stores an object after recomputing its ref from the received bytes;
// a client ref that does not match is rejected.
func (r *RemoteServer) Put(ctx context.Context, request *proto.PutRequest) (*proto.PutResponse, error) {
	payload, err := request.GetObject().Canonical()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	ref := proto.HashPayload(request.Object.Type(), payload)
	if request.Ref != nil && !ref.Equal(request.Ref) {
		return nil, status.Errorf(codes.InvalidArgument, "ref mismatch: client sent %x, server computed %x", request.Ref.Hash, ref.Hash)
	}

	err = r.index.Put(ctx, request.Object)
	if errors.Is(err, backup.ErrDanglingRef) {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	return &proto.PutResponse{}, err
}

// LatestCommit answers with an empty ref when the set has no commit.
func (r *RemoteServer) LatestCommit(ctx context.Context, request *proto.LatestCommitRequest) (*proto.LatestCommitResponse, error) {
	ref, err := r.index.LatestCommit(ctx, request.BackupSet)
	if errors.Is(err, backup.ErrNotFound) {
		return &proto.LatestCommitResponse{}, nil
	}

	if err != nil {
		return nil, err
	}

	return &proto.LatestCommitResponse{Ref: ref}, nil
}

// GetTree streams the tree at ref, its splits, and the trees of directories
// below it down to max_depth levels, breadth-first.
func (r *RemoteServer) GetTree(request *proto.GetTreeRequest, stream proto.Store_GetTreeServer) error {
	ctx := stream.Context()

	type pending struct {
		ref   *proto.Ref
		depth uint32
	}

	queue := []pending{{ref: request.Ref}}

	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]

		obj, err := r.index.Get(ctx, next.ref)
		if errors.Is(err, backup.ErrNotFound) {
			return status.Errorf(codes.NotFound, "tree %x not found", next.ref.GetHash())
		}

		if err != nil {
			return err
		}

		tree := obj.GetTree()
		if tree == nil {
			return status.Errorf(codes.InvalidArgument, "object %x is not a tree", next.ref.GetHash())
		}

		err = stream.Send(&proto.GetTreeResponse{Ref: next.ref, Object: obj})
		if err != nil {
			return err
		}

		for _, split := range tree.Splits {
			queue = append(queue, pending{ref: split, depth: next.depth})
		}

		if next.depth >= request.MaxDepth {
			continue
		}

		for _, node := range tree.Nodes {
			if node.GetStat().IsDir() {
				queue = append(queue, pending{ref: node.Ref, depth: next.depth + 1})
			}
		}
	}

	return nil
}

func (r *RemoteServer) Get(ctx context.Context, request *proto.GetRequest) (*proto.GetResponse, error) {
	obj, err := r.index.Get(ctx, request.Ref)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "object %x not found", request.Ref.GetHash())
	}

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
	return r.index.Walk(walker.Context(), request.Load, request.ObjectType, func(object *proto.Object) error {
		return walker.Send(&proto.WalkResponse{Object: object})
	})
}

func (r *RemoteServer) Has(ctx context.Context, request *proto.HasRequest) (*proto.HasResponse, error) {
	has, err := r.index.Has(ctx, request.Ref)

	if err != nil {
		return nil, err
	}

	return &proto.HasResponse{Has: has}, nil
}
