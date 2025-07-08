package downloadtasktransport

import (
	"context"
	"github.com/go-kit/kit/transport"
	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/yuisofull/goload/internal/downloadtask"
	downloadtaskendpoint "github.com/yuisofull/goload/internal/downloadtask/endpoint"
	"github.com/yuisofull/goload/internal/downloadtask/pb"
	"github.com/yuisofull/goload/internal/errors"
	"google.golang.org/grpc"
)

type gRPCServer struct {
	pb.UnimplementedDownloadTaskServiceServer
	createDownloadTask  grpctransport.Handler
	getDownloadTaskList grpctransport.Handler
	updateDownloadTask  grpctransport.Handler
	deleteDownloadTask  grpctransport.Handler
}

func NewGRPCServer(endpoints downloadtaskendpoint.Set, logger log.Logger) pb.DownloadTaskServiceServer {
	options := []grpctransport.ServerOption{
		grpctransport.ServerErrorHandler(transport.NewLogErrorHandler(level.Error(logger))),
	}

	return &gRPCServer{
		createDownloadTask: grpctransport.NewServer(
			endpoints.CreateDownloadTaskEndpoint,
			decodeCreateDownloadTaskRequest,
			encodeCreateDownloadTaskResponse,
			options...,
		),
		getDownloadTaskList: grpctransport.NewServer(
			endpoints.GetDownloadTaskListEndpoint,
			decodeGetDownloadTaskListRequest,
			encodeGetDownloadTaskListResponse,
			options...,
		),
		updateDownloadTask: grpctransport.NewServer(
			endpoints.UpdateDownloadTaskEndpoint,
			decodeUpdateDownloadTaskRequest,
			encodeUpdateDownloadTaskResponse,
			options...,
		),
		deleteDownloadTask: grpctransport.NewServer(
			endpoints.DeleteDownloadTaskEndpoint,
			decodeDeleteDownloadTaskRequest,
			encodeDeleteDownloadTaskResponse,
			options...,
		),
	}
}

func NewGRPCClient(conn *grpc.ClientConn, logger log.Logger) downloadtask.Service {
	options := []grpctransport.ClientOption{
		grpctransport.ClientBefore(NewLogRequestFunc(logger)),
		grpctransport.ClientAfter(NewLogResponseFunc(logger)),
	}
	return &downloadtaskendpoint.Set{
		CreateDownloadTaskEndpoint: grpctransport.NewClient(
			conn,
			"pb.DownloadTaskService",
			"CreateDownloadTask",
			encodeCreateDownloadTaskRequest,
			decodeCreateDownloadTaskResponse,
			pb.CreateDownloadTaskResponse{},
			options...,
		).Endpoint(),
		GetDownloadTaskListEndpoint: grpctransport.NewClient(
			conn,
			"pb.DownloadTaskService",
			"GetDownloadTaskList",
			encodeGetDownloadTaskListRequest,
			decodeGetDownloadTaskListResponse,
			pb.GetDownloadTaskListResponse{},
			options...,
		).Endpoint(),
		UpdateDownloadTaskEndpoint: grpctransport.NewClient(
			conn,
			"pb.DownloadTaskService",
			"UpdateDownloadTask",
			encodeUpdateDownloadTaskRequest,
			decodeUpdateDownloadTaskResponse,
			pb.UpdateDownloadTaskResponse{},
			options...,
		).Endpoint(),
		DeleteDownloadTaskEndpoint: grpctransport.NewClient(
			conn,
			"pb.DownloadTaskService",
			"DeleteDownloadTask",
			encodeDeleteDownloadTaskRequest,
			decodeDeleteDownloadTaskResponse,
			pb.DeleteDownloadTaskResponse{},
			options...,
		).Endpoint(),
	}
}

func encodeError(_ context.Context, err error) error {
	return errors.EncodeGRPCError(err)
}

func (s *gRPCServer) CreateDownloadTask(ctx context.Context, req *pb.CreateDownloadTaskRequest) (*pb.CreateDownloadTaskResponse, error) {
	_, resp, err := s.createDownloadTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.CreateDownloadTaskResponse), nil
}

func (s *gRPCServer) GetDownloadTaskList(ctx context.Context, req *pb.GetDownloadTaskListRequest) (*pb.GetDownloadTaskListResponse, error) {
	_, resp, err := s.getDownloadTaskList.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.GetDownloadTaskListResponse), nil
}

func (s *gRPCServer) UpdateDownloadTask(ctx context.Context, req *pb.UpdateDownloadTaskRequest) (*pb.UpdateDownloadTaskResponse, error) {
	_, resp, err := s.updateDownloadTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.UpdateDownloadTaskResponse), nil
}

func (s *gRPCServer) DeleteDownloadTask(ctx context.Context, req *pb.DeleteDownloadTaskRequest) (*pb.DeleteDownloadTaskResponse, error) {
	_, resp, err := s.deleteDownloadTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.DeleteDownloadTaskResponse), nil
}

func decodeCreateDownloadTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.CreateDownloadTaskRequest)
	return &downloadtaskendpoint.CreateDownloadTaskRequest{
		Token:        req.Token,
		DownloadType: req.DownloadType,
		Url:          req.Url,
	}, nil
}

func encodeCreateDownloadTaskResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*downloadtaskendpoint.CreateDownloadTaskResponse)
	return &pb.CreateDownloadTaskResponse{
		DownloadTask: &pb.DownloadTask{
			Id:             resp.DownloadTask.Id,
			OfAccountId:    resp.DownloadTask.OfAccountId,
			DownloadType:   resp.DownloadTask.DownloadType,
			Url:            resp.DownloadTask.Url,
			DownloadStatus: resp.DownloadTask.DownloadStatus,
		},
	}, nil
}

func decodeGetDownloadTaskListRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.GetDownloadTaskListRequest)
	return &downloadtaskendpoint.GetDownloadTaskListRequest{
		Token:  req.Token,
		Offset: req.Offset,
		Limit:  req.Limit,
	}, nil
}

func encodeGetDownloadTaskListResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*downloadtaskendpoint.GetDownloadTaskListResponse)
	pbTasks := make([]*pb.DownloadTask, len(resp.DownloadTasks))
	for i, task := range resp.DownloadTasks {
		pbTasks[i] = &pb.DownloadTask{
			Id:             task.Id,
			OfAccountId:    task.OfAccountId,
			DownloadType:   task.DownloadType,
			Url:            task.Url,
			DownloadStatus: task.DownloadStatus,
		}
	}
	return &pb.GetDownloadTaskListResponse{
		DownloadTasks: pbTasks,
		TotalCount:    resp.TotalCount,
	}, nil
}

func decodeUpdateDownloadTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.UpdateDownloadTaskRequest)
	return &downloadtaskendpoint.UpdateDownloadTaskRequest{
		Token:          req.Token,
		DownloadTaskId: req.DownloadTaskId,
		Url:            req.Url,
	}, nil
}

func encodeUpdateDownloadTaskResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*downloadtaskendpoint.UpdateDownloadTaskResponse)
	return &pb.UpdateDownloadTaskResponse{
		DownloadTask: &pb.DownloadTask{
			Id:             resp.DownloadTask.Id,
			OfAccountId:    resp.DownloadTask.OfAccountId,
			DownloadType:   resp.DownloadTask.DownloadType,
			Url:            resp.DownloadTask.Url,
			DownloadStatus: resp.DownloadTask.DownloadStatus,
		},
	}, nil
}

func decodeDeleteDownloadTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.DeleteDownloadTaskRequest)
	return &downloadtaskendpoint.DeleteDownloadTaskRequest{
		Token:          req.Token,
		DownloadTaskId: req.DownloadTaskId,
	}, nil
}

func encodeDeleteDownloadTaskResponse(_ context.Context, response interface{}) (interface{}, error) {
	return &pb.DeleteDownloadTaskResponse{}, nil
}

func encodeCreateDownloadTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*downloadtaskendpoint.CreateDownloadTaskRequest)
	return &pb.CreateDownloadTaskRequest{
		Token:        req.Token,
		DownloadType: req.DownloadType,
		Url:          req.Url,
	}, nil
}

func decodeCreateDownloadTaskResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.CreateDownloadTaskResponse)
	return &downloadtaskendpoint.CreateDownloadTaskResponse{
		DownloadTask: &pb.DownloadTask{
			Id:             resp.DownloadTask.Id,
			OfAccountId:    resp.DownloadTask.OfAccountId,
			DownloadType:   resp.DownloadTask.DownloadType,
			Url:            resp.DownloadTask.Url,
			DownloadStatus: resp.DownloadTask.DownloadStatus,
		},
	}, nil
}

func encodeGetDownloadTaskListRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*downloadtaskendpoint.GetDownloadTaskListRequest)
	return &pb.GetDownloadTaskListRequest{
		Token:  req.Token,
		Offset: req.Offset,
		Limit:  req.Limit,
	}, nil
}

func decodeGetDownloadTaskListResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.GetDownloadTaskListResponse)
	pbTasks := resp.GetDownloadTasks()
	return &downloadtaskendpoint.GetDownloadTaskListResponse{
		DownloadTasks: pbTasks,
		TotalCount:    resp.GetTotalCount(),
	}, nil
}

func encodeUpdateDownloadTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*downloadtaskendpoint.UpdateDownloadTaskRequest)
	return &pb.UpdateDownloadTaskRequest{
		Token:          req.Token,
		DownloadTaskId: req.DownloadTaskId,
		Url:            req.Url,
	}, nil
}

func decodeUpdateDownloadTaskResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.UpdateDownloadTaskResponse)
	return &downloadtaskendpoint.UpdateDownloadTaskResponse{
		DownloadTask: &pb.DownloadTask{
			Id:             resp.DownloadTask.Id,
			OfAccountId:    resp.DownloadTask.OfAccountId,
			DownloadType:   resp.DownloadTask.DownloadType,
			Url:            resp.DownloadTask.Url,
			DownloadStatus: resp.DownloadTask.DownloadStatus,
		},
	}, nil
}

func encodeDeleteDownloadTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*downloadtaskendpoint.DeleteDownloadTaskRequest)
	return &pb.DeleteDownloadTaskRequest{
		Token:          req.Token,
		DownloadTaskId: req.DownloadTaskId,
	}, nil
}

func decodeDeleteDownloadTaskResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	return &downloadtaskendpoint.DeleteDownloadTaskResponse{}, nil
}
