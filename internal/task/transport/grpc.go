package tasktransport

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/kit/transport"
	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/task"
	taskendpoint "github.com/yuisofull/goload/internal/task/endpoint"
	pb "github.com/yuisofull/goload/internal/task/pb"
	"google.golang.org/grpc"
)

type grpcServer struct {
	pb.UnimplementedTaskServiceServer
	createTask            grpctransport.Handler
	getTask               grpctransport.Handler
	listTasks             grpctransport.Handler
	deleteTask            grpctransport.Handler
	startTask             grpctransport.Handler
	pauseTask             grpctransport.Handler
	resumeTask            grpctransport.Handler
	cancelTask            grpctransport.Handler
	retryTask             grpctransport.Handler
	updateTaskStoragePath grpctransport.Handler
	updateTaskStatus      grpctransport.Handler
	updateTaskProgress    grpctransport.Handler
	updateTaskError       grpctransport.Handler
	completeTask          grpctransport.Handler
	getFileInfo           grpctransport.Handler
	checkFileExists       grpctransport.Handler
	getTaskProgress       grpctransport.Handler
}

func (s *grpcServer) CreateTask(ctx context.Context, req *pb.CreateTaskRequest) (*pb.TaskResponse, error) {
	_, resp, err := s.createTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.TaskResponse), nil
}

func (s *grpcServer) GetTask(ctx context.Context, req *pb.GetTaskRequest) (*pb.TaskResponse, error) {
	_, resp, err := s.getTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.TaskResponse), nil
}

func (s *grpcServer) UpdateTaskStoragePath(ctx context.Context, req *pb.UpdateTaskStoragePathRequest) (*pb.TaskResponse, error) {
	_, resp, err := s.updateTaskStoragePath.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.TaskResponse), nil
}

func (s *grpcServer) UpdateTaskStatus(ctx context.Context, req *pb.UpdateTaskStatusRequest) (*pb.TaskResponse, error) {
	_, resp, err := s.updateTaskStatus.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.TaskResponse), nil
}

func (s *grpcServer) UpdateTaskProgress(ctx context.Context, req *pb.UpdateTaskProgressRequest) (*pb.TaskResponse, error) {
	_, resp, err := s.updateTaskProgress.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.TaskResponse), nil
}

func (s *grpcServer) UpdateTaskError(ctx context.Context, req *pb.UpdateTaskErrorRequest) (*pb.TaskResponse, error) {
	_, resp, err := s.updateTaskError.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.TaskResponse), nil
}

func (s *grpcServer) CompleteTask(ctx context.Context, req *pb.CompleteTaskRequest) (*pb.TaskResponse, error) {
	_, resp, err := s.completeTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.TaskResponse), nil
}

func (s *grpcServer) ListTasks(ctx context.Context, req *pb.ListTasksRequest) (*pb.ListTasksResponse, error) {
	_, resp, err := s.listTasks.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.ListTasksResponse), nil
}

func (s *grpcServer) DeleteTask(ctx context.Context, req *pb.DeleteTaskRequest) (*pb.DeleteTaskResponse, error) {
	_, resp, err := s.deleteTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.DeleteTaskResponse), nil
}

func (s *grpcServer) StartTask(ctx context.Context, req *pb.StartTaskRequest) (*pb.StartTaskResponse, error) {
	_, resp, err := s.startTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.StartTaskResponse), nil
}

func (s *grpcServer) PauseTask(ctx context.Context, req *pb.PauseTaskRequest) (*pb.PauseTaskResponse, error) {
	_, resp, err := s.pauseTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.PauseTaskResponse), nil
}

func (s *grpcServer) ResumeTask(ctx context.Context, req *pb.ResumeTaskRequest) (*pb.ResumeTaskResponse, error) {
	_, resp, err := s.resumeTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.ResumeTaskResponse), nil
}

func (s *grpcServer) CancelTask(ctx context.Context, req *pb.CancelTaskRequest) (*pb.CancelTaskResponse, error) {
	_, resp, err := s.cancelTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.CancelTaskResponse), nil
}

func (s *grpcServer) RetryTask(ctx context.Context, req *pb.RetryTaskRequest) (*pb.RetryTaskResponse, error) {
	_, resp, err := s.retryTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.RetryTaskResponse), nil
}

func (s *grpcServer) GetFileInfo(ctx context.Context, req *pb.GetFileInfoRequest) (*pb.GetFileInfoResponse, error) {
	_, resp, err := s.getFileInfo.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.GetFileInfoResponse), nil
}

func (s *grpcServer) CheckFileExists(ctx context.Context, req *pb.CheckFileExistsRequest) (*pb.CheckFileExistsResponse, error) {
	_, resp, err := s.checkFileExists.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.CheckFileExistsResponse), nil
}

func (s *grpcServer) GetTaskProgress(ctx context.Context, req *pb.GetTaskProgressRequest) (*pb.GetTaskProgressResponse, error) {
	_, resp, err := s.getTaskProgress.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.GetTaskProgressResponse), nil
}

func encodeError(_ context.Context, err error) error {
	return errors.EncodeGRPCError(err)
}

func NewGRPCServer(endpoints taskendpoint.Set, logger log.Logger) pb.TaskServiceServer {
	options := []grpctransport.ServerOption{
		grpctransport.ServerErrorHandler(transport.NewLogErrorHandler(level.Error(logger))),
	}

	return &grpcServer{
		createTask:            grpctransport.NewServer(endpoints.CreateTaskEndpoint, decodeCreateTaskRequest, encodeTaskResponse, options...),
		getTask:               grpctransport.NewServer(endpoints.GetTaskEndpoint, decodeGetTaskRequest, encodeTaskResponse, options...),
		listTasks:             grpctransport.NewServer(endpoints.ListTasksEndpoint, decodeListTasksRequest, encodeListTasksResponse, options...),
		deleteTask:            grpctransport.NewServer(endpoints.DeleteTaskEndpoint, decodeDeleteTaskRequest, encodeDeleteTaskResponse, options...),
		startTask:             grpctransport.NewServer(endpoints.StartTaskEndpoint, decodeStartTaskRequest, encodeStartTaskResponse, options...),
		pauseTask:             grpctransport.NewServer(endpoints.PauseTaskEndpoint, decodePauseTaskRequest, encodePauseTaskResponse, options...),
		resumeTask:            grpctransport.NewServer(endpoints.ResumeTaskEndpoint, decodeResumeTaskRequest, encodeResumeTaskResponse, options...),
		cancelTask:            grpctransport.NewServer(endpoints.CancelTaskEndpoint, decodeCancelTaskRequest, encodeCancelTaskResponse, options...),
		retryTask:             grpctransport.NewServer(endpoints.RetryTaskEndpoint, decodeRetryTaskRequest, encodeRetryTaskResponse, options...),
		updateTaskStoragePath: grpctransport.NewServer(endpoints.UpdateTaskStoragePathEndpoint, decodeUpdateTaskStoragePathRequest, encodeTaskResponse, options...),
		updateTaskStatus:      grpctransport.NewServer(endpoints.UpdateTaskStatusEndpoint, decodeUpdateTaskStatusRequest, encodeTaskResponse, options...),
		updateTaskProgress:    grpctransport.NewServer(endpoints.UpdateTaskProgressEndpoint, decodeUpdateTaskProgressRequest, encodeTaskResponse, options...),
		updateTaskError:       grpctransport.NewServer(endpoints.UpdateTaskErrorEndpoint, decodeUpdateTaskErrorRequest, encodeTaskResponse, options...),
		completeTask:          grpctransport.NewServer(endpoints.CompleteTaskEndpoint, decodeCompleteTaskRequest, encodeTaskResponse, options...),
		getFileInfo:           grpctransport.NewServer(endpoints.GetFileInfoEndpoint, decodeGetFileInfoRequest, encodeGetFileInfoResponse, options...),
		checkFileExists:       grpctransport.NewServer(endpoints.CheckFileExistsEndpoint, decodeCheckFileExistsRequest, encodeCheckFileExistsResponse, options...),
		getTaskProgress:       grpctransport.NewServer(endpoints.GetTaskProgressEndpoint, decodeGetTaskProgressRequest, encodeGetTaskProgressResponse, options...),
	}
}

func NewGRPCClient(conn *grpc.ClientConn, logger log.Logger) task.Service {
	options := []grpctransport.ClientOption{
		grpctransport.ClientBefore(NewLogRequestFunc(logger)),
		grpctransport.ClientAfter(NewLogResponseFunc(logger)),
	}
	return &taskendpoint.Set{
		CreateTaskEndpoint:            grpctransport.NewClient(conn, "pb.TaskService", "CreateTask", encodeCreateTaskRequest, decodeTaskResponse, pb.TaskResponse{}, options...).Endpoint(),
		GetTaskEndpoint:               grpctransport.NewClient(conn, "pb.TaskService", "GetTask", encodeGetTaskRequest, decodeTaskResponse, pb.TaskResponse{}, options...).Endpoint(),
		ListTasksEndpoint:             grpctransport.NewClient(conn, "pb.TaskService", "ListTasks", encodeListTasksRequest, decodeListTasksResponse, pb.ListTasksResponse{}, options...).Endpoint(),
		DeleteTaskEndpoint:            grpctransport.NewClient(conn, "pb.TaskService", "DeleteTask", encodeDeleteTaskRequest, decodeDeleteTaskResponse, pb.DeleteTaskResponse{}, options...).Endpoint(),
		StartTaskEndpoint:             grpctransport.NewClient(conn, "pb.TaskService", "StartTask", encodeStartTaskRequest, decodeStartTaskResponse, pb.StartTaskResponse{}, options...).Endpoint(),
		PauseTaskEndpoint:             grpctransport.NewClient(conn, "pb.TaskService", "PauseTask", encodePauseTaskRequest, decodePauseTaskResponse, pb.PauseTaskResponse{}, options...).Endpoint(),
		ResumeTaskEndpoint:            grpctransport.NewClient(conn, "pb.TaskService", "ResumeTask", encodeResumeTaskRequest, decodeResumeTaskResponse, pb.ResumeTaskResponse{}, options...).Endpoint(),
		CancelTaskEndpoint:            grpctransport.NewClient(conn, "pb.TaskService", "CancelTask", encodeCancelTaskRequest, decodeCancelTaskResponse, pb.CancelTaskResponse{}, options...).Endpoint(),
		RetryTaskEndpoint:             grpctransport.NewClient(conn, "pb.TaskService", "RetryTask", encodeRetryTaskRequest, decodeRetryTaskResponse, pb.RetryTaskResponse{}, options...).Endpoint(),
		UpdateTaskStoragePathEndpoint: grpctransport.NewClient(conn, "pb.TaskService", "UpdateTaskStoragePath", encodeUpdateTaskStoragePathRequest, decodeTaskResponse, pb.TaskResponse{}, options...).Endpoint(),
		UpdateTaskStatusEndpoint:      grpctransport.NewClient(conn, "pb.TaskService", "UpdateTaskStatus", encodeUpdateTaskStatusRequest, decodeTaskResponse, pb.TaskResponse{}, options...).Endpoint(),
		UpdateTaskProgressEndpoint:    grpctransport.NewClient(conn, "pb.TaskService", "UpdateTaskProgress", encodeUpdateTaskProgressRequest, decodeTaskResponse, pb.TaskResponse{}, options...).Endpoint(),
		UpdateTaskErrorEndpoint:       grpctransport.NewClient(conn, "pb.TaskService", "UpdateTaskError", encodeUpdateTaskErrorRequest, decodeTaskResponse, pb.TaskResponse{}, options...).Endpoint(),
		CompleteTaskEndpoint:          grpctransport.NewClient(conn, "pb.TaskService", "CompleteTask", encodeCompleteTaskRequest, decodeTaskResponse, pb.TaskResponse{}, options...).Endpoint(),
		GetFileInfoEndpoint:           grpctransport.NewClient(conn, "pb.TaskService", "GetFileInfo", encodeGetFileInfoRequest, decodeGetFileInfoResponse, pb.GetFileInfoResponse{}, options...).Endpoint(),
		CheckFileExistsEndpoint:       grpctransport.NewClient(conn, "pb.TaskService", "CheckFileExists", encodeCheckFileExistsRequest, decodeCheckFileExistsResponse, pb.CheckFileExistsResponse{}, options...).Endpoint(),
		GetTaskProgressEndpoint:       grpctransport.NewClient(conn, "pb.TaskService", "GetTaskProgress", encodeGetTaskProgressRequest, decodeGetTaskProgressResponse, pb.GetTaskProgressResponse{}, options...).Endpoint(),
	}
}

// Server-side decoders
func decodeCreateTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.CreateTaskRequest)
	return (*taskendpoint.CreateTaskRequest)(req), nil
}
func decodeGetTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.GetTaskRequest)
	return (*taskendpoint.GetTaskRequest)(req), nil
}
func decodeUpdateTaskStoragePathRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.UpdateTaskStoragePathRequest)
	return (*taskendpoint.UpdateTaskStoragePathRequest)(req), nil
}
func decodeUpdateTaskStatusRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.UpdateTaskStatusRequest)
	return (*taskendpoint.UpdateTaskStatusRequest)(req), nil
}
func decodeUpdateTaskProgressRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.UpdateTaskProgressRequest)
	return (*taskendpoint.UpdateTaskProgressRequest)(req), nil
}
func decodeUpdateTaskErrorRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.UpdateTaskErrorRequest)
	return (*taskendpoint.UpdateTaskErrorRequest)(req), nil
}
func decodeCompleteTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.CompleteTaskRequest)
	return (*taskendpoint.CompleteTaskRequest)(req), nil
}
func decodeListTasksRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.ListTasksRequest)
	return (*taskendpoint.ListTasksRequest)(req), nil
}
func decodeDeleteTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.DeleteTaskRequest)
	return (*taskendpoint.DeleteTaskRequest)(req), nil
}
func decodeStartTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.StartTaskRequest)
	return (*taskendpoint.StartTaskRequest)(req), nil
}
func decodePauseTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.PauseTaskRequest)
	return (*taskendpoint.PauseTaskRequest)(req), nil
}
func decodeResumeTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.ResumeTaskRequest)
	return (*taskendpoint.ResumeTaskRequest)(req), nil
}
func decodeCancelTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.CancelTaskRequest)
	return (*taskendpoint.CancelTaskRequest)(req), nil
}
func decodeRetryTaskRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.RetryTaskRequest)
	return (*taskendpoint.RetryTaskRequest)(req), nil
}
func decodeGetFileInfoRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.GetFileInfoRequest)
	return (*taskendpoint.GetFileInfoRequest)(req), nil
}
func decodeCheckFileExistsRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.CheckFileExistsRequest)
	return (*taskendpoint.CheckFileExistsRequest)(req), nil
}
func decodeGetTaskProgressRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.GetTaskProgressRequest)
	return (*taskendpoint.GetTaskProgressRequest)(req), nil
}

// Server-side encoders
func encodeTaskResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.TaskResponse)
	return (*pb.TaskResponse)(resp), nil
}
func encodeListTasksResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.ListTasksResponse)
	return (*pb.ListTasksResponse)(resp), nil
}
func encodeDeleteTaskResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.DeleteTaskResponse)
	return (*pb.DeleteTaskResponse)(resp), nil
}
func encodeStartTaskResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.StartTaskResponse)
	return (*pb.StartTaskResponse)(resp), nil
}
func encodePauseTaskResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.PauseTaskResponse)
	return (*pb.PauseTaskResponse)(resp), nil
}
func encodeResumeTaskResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.ResumeTaskResponse)
	return (*pb.ResumeTaskResponse)(resp), nil
}
func encodeCancelTaskResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.CancelTaskResponse)
	return (*pb.CancelTaskResponse)(resp), nil
}
func encodeRetryTaskResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.RetryTaskResponse)
	return (*pb.RetryTaskResponse)(resp), nil
}
func encodeGetFileInfoResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.GetFileInfoResponse)
	return (*pb.GetFileInfoResponse)(resp), nil
}
func encodeCheckFileExistsResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.CheckFileExistsResponse)
	return (*pb.CheckFileExistsResponse)(resp), nil
}
func encodeGetTaskProgressResponse(_ context.Context, response interface{}) (interface{}, error) {
	resp := response.(*taskendpoint.GetTaskProgressResponse)
	return (*pb.GetTaskProgressResponse)(resp), nil
}

// Client-side encoders
func encodeCreateTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.CreateTaskRequest)
	return (*pb.CreateTaskRequest)(req), nil
}
func encodeGetTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.GetTaskRequest)
	return (*pb.GetTaskRequest)(req), nil
}
func encodeUpdateTaskStoragePathRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.UpdateTaskStoragePathRequest)
	return (*pb.UpdateTaskStoragePathRequest)(req), nil
}
func encodeUpdateTaskStatusRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.UpdateTaskStatusRequest)
	return (*pb.UpdateTaskStatusRequest)(req), nil
}
func encodeUpdateTaskProgressRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.UpdateTaskProgressRequest)
	return (*pb.UpdateTaskProgressRequest)(req), nil
}
func encodeUpdateTaskErrorRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.UpdateTaskErrorRequest)
	return (*pb.UpdateTaskErrorRequest)(req), nil
}
func encodeCompleteTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.CompleteTaskRequest)
	return (*pb.CompleteTaskRequest)(req), nil
}
func encodeListTasksRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.ListTasksRequest)
	return (*pb.ListTasksRequest)(req), nil
}
func encodeDeleteTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.DeleteTaskRequest)
	return (*pb.DeleteTaskRequest)(req), nil
}
func encodeStartTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.StartTaskRequest)
	return (*pb.StartTaskRequest)(req), nil
}
func encodePauseTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.PauseTaskRequest)
	return (*pb.PauseTaskRequest)(req), nil
}
func encodeResumeTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.ResumeTaskRequest)
	return (*pb.ResumeTaskRequest)(req), nil
}
func encodeCancelTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.CancelTaskRequest)
	return (*pb.CancelTaskRequest)(req), nil
}
func encodeRetryTaskRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.RetryTaskRequest)
	return (*pb.RetryTaskRequest)(req), nil
}
func encodeGetFileInfoRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.GetFileInfoRequest)
	return (*pb.GetFileInfoRequest)(req), nil
}
func encodeCheckFileExistsRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.CheckFileExistsRequest)
	return (*pb.CheckFileExistsRequest)(req), nil
}
func encodeGetTaskProgressRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(*taskendpoint.GetTaskProgressRequest)
	return (*pb.GetTaskProgressRequest)(req), nil
}

// Client-side decoders
func decodeTaskResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.TaskResponse)
	return (*taskendpoint.TaskResponse)(resp), nil
}
func decodeListTasksResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.ListTasksResponse)
	return (*taskendpoint.ListTasksResponse)(resp), nil
}
func decodeDeleteTaskResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.DeleteTaskResponse)
	return (*taskendpoint.DeleteTaskResponse)(resp), nil
}
func decodeStartTaskResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.StartTaskResponse)
	return (*taskendpoint.StartTaskResponse)(resp), nil
}
func decodePauseTaskResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.PauseTaskResponse)
	return (*taskendpoint.PauseTaskResponse)(resp), nil
}
func decodeResumeTaskResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.ResumeTaskResponse)
	return (*taskendpoint.ResumeTaskResponse)(resp), nil
}
func decodeCancelTaskResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.CancelTaskResponse)
	return (*taskendpoint.CancelTaskResponse)(resp), nil
}
func decodeRetryTaskResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.RetryTaskResponse)
	return (*taskendpoint.RetryTaskResponse)(resp), nil
}
func decodeGetFileInfoResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.GetFileInfoResponse)
	return (*taskendpoint.GetFileInfoResponse)(resp), nil
}
func decodeCheckFileExistsResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.CheckFileExistsResponse)
	return (*taskendpoint.CheckFileExistsResponse)(resp), nil
}
func decodeGetTaskProgressResponse(_ context.Context, grpcResp interface{}) (interface{}, error) {
	resp := grpcResp.(*pb.GetTaskProgressResponse)
	return (*taskendpoint.GetTaskProgressResponse)(resp), nil
}
