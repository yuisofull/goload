package taskendpoint

import (
	"context"
	stderrors "errors"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/ratelimit"
	"github.com/yuisofull/goload/internal/task"
	pb "github.com/yuisofull/goload/internal/task/pb"
	"golang.org/x/time/rate"
)

// Alias protobuf messages to endpoint layer request/response types

type CreateTaskRequest pb.CreateTaskRequest

type TaskResponse pb.TaskResponse

type GetTaskRequest pb.GetTaskRequest

type UpdateTaskStoragePathRequest pb.UpdateTaskStoragePathRequest

type UpdateTaskStatusRequest pb.UpdateTaskStatusRequest

type UpdateTaskProgressRequest pb.UpdateTaskProgressRequest

type UpdateTaskErrorRequest pb.UpdateTaskErrorRequest

type CompleteTaskRequest pb.CompleteTaskRequest

type ListTasksRequest pb.ListTasksRequest

type ListTasksResponse pb.ListTasksResponse

type DeleteTaskRequest pb.DeleteTaskRequest

type DeleteTaskResponse pb.DeleteTaskResponse

type PauseTaskRequest pb.PauseTaskRequest

type PauseTaskResponse pb.PauseTaskResponse

type ResumeTaskRequest pb.ResumeTaskRequest

type ResumeTaskResponse pb.ResumeTaskResponse

type CancelTaskRequest pb.CancelTaskRequest

type CancelTaskResponse pb.CancelTaskResponse

type RetryTaskRequest pb.RetryTaskRequest

type RetryTaskResponse pb.RetryTaskResponse

type CheckFileExistsRequest pb.CheckFileExistsRequest

type CheckFileExistsResponse pb.CheckFileExistsResponse

type GetTaskProgressRequest pb.GetTaskProgressRequest

type GetTaskProgressResponse pb.GetTaskProgressResponse

type UpdateTaskChecksumRequest struct {
	TaskId   uint64
	Checksum *pb.ChecksumInfo
}

type UpdateTaskMetadataRequest struct {
	TaskId   uint64
	Metadata *structpb.Struct
}

// Set contains all endpoints for the Task Service

type Set struct {
	CreateTaskEndpoint endpoint.Endpoint
	GetTaskEndpoint    endpoint.Endpoint
	ListTasksEndpoint  endpoint.Endpoint
	DeleteTaskEndpoint endpoint.Endpoint

	PauseTaskEndpoint  endpoint.Endpoint
	ResumeTaskEndpoint endpoint.Endpoint
	CancelTaskEndpoint endpoint.Endpoint
	RetryTaskEndpoint  endpoint.Endpoint

	UpdateTaskStoragePathEndpoint endpoint.Endpoint
	UpdateTaskStatusEndpoint      endpoint.Endpoint
	UpdateTaskProgressEndpoint    endpoint.Endpoint
	UpdateTaskErrorEndpoint       endpoint.Endpoint
	CompleteTaskEndpoint          endpoint.Endpoint

	UpdateTaskChecksumEndpoint endpoint.Endpoint
	UpdateTaskMetadataEndpoint endpoint.Endpoint

	CheckFileExistsEndpoint endpoint.Endpoint
	GetTaskProgressEndpoint endpoint.Endpoint
}

// Implement task.Service on the Set for GRPC client usage

func (e *Set) CreateTask(ctx context.Context, param *task.CreateTaskParam) (*task.Task, error) {
	resp, err := e.CreateTaskEndpoint(ctx, &CreateTaskRequest{
		OfAccountId: param.OfAccountID,
		FileName:    param.FileName,
		SourceUrl:   param.SourceURL,
		SourceType:  pb.SourceType(pb.SourceType_value[string(param.SourceType)]),
		SourceAuth:  toPBAuthConfig(param.SourceAuth),
		Checksum: &pb.ChecksumInfo{
			ChecksumType:  param.Checksum.ChecksumType,
			ChecksumValue: param.Checksum.ChecksumValue,
		},
		Metadata: toPBStruct(param.Metadata),
	})
	if err != nil {
		return nil, err
	}
	out := resp.(*TaskResponse)
	return fromPBTask(out.Task), nil
}

func (e *Set) GetTask(ctx context.Context, id uint64) (*task.Task, error) {
	resp, err := e.GetTaskEndpoint(ctx, &GetTaskRequest{Id: id})
	if err != nil {
		return nil, err
	}
	out := resp.(*TaskResponse)
	return fromPBTask(out.Task), nil
}

func (e *Set) ListTasks(ctx context.Context, param *task.ListTasksParam) (*task.ListTasksOutput, error) {
	resp, err := e.ListTasksEndpoint(ctx, &ListTasksRequest{
		Filter:  &pb.TaskFilter{OfAccountId: param.Filter.OfAccountID},
		Offset:  param.Offset,
		Limit:   param.Limit,
		SortBy:  param.SortBy,
		SortAsc: param.SortAsc,
	})
	if err != nil {
		return nil, err
	}
	out := resp.(*ListTasksResponse)
	res := &task.ListTasksOutput{Total: out.Total}
	for _, t := range out.Tasks {
		res.Tasks = append(res.Tasks, fromPBTask(t))
	}
	return res, nil
}

func (e *Set) DeleteTask(ctx context.Context, id uint64) error {
	_, err := e.DeleteTaskEndpoint(ctx, &DeleteTaskRequest{Id: id})
	return err
}

func (e *Set) PauseTask(ctx context.Context, taskID uint64) error {
	_, err := e.PauseTaskEndpoint(ctx, &PauseTaskRequest{Id: taskID})
	return err
}

func (e *Set) ResumeTask(ctx context.Context, taskID uint64) error {
	_, err := e.ResumeTaskEndpoint(ctx, &ResumeTaskRequest{Id: taskID})
	return err
}

func (e *Set) CancelTask(ctx context.Context, taskID uint64) error {
	_, err := e.CancelTaskEndpoint(ctx, &CancelTaskRequest{Id: taskID})
	return err
}

func (e *Set) RetryTask(ctx context.Context, taskID uint64) error {
	_, err := e.RetryTaskEndpoint(ctx, &RetryTaskRequest{Id: taskID})
	return err
}

func (e *Set) UpdateTaskStoragePath(ctx context.Context, id uint64, storagePath string) error {
	_, err := e.UpdateTaskStoragePathEndpoint(ctx, &UpdateTaskStoragePathRequest{Id: id, StoragePath: storagePath})
	return err
}

func (e *Set) UpdateTaskStatus(ctx context.Context, id uint64, status task.TaskStatus) error {
	_, err := e.UpdateTaskStatusEndpoint(ctx, &UpdateTaskStatusRequest{Id: id, Status: pb.TaskStatus(pb.TaskStatus_value[string(status)])})
	return err
}

func (e *Set) UpdateTaskProgress(ctx context.Context, id uint64, progress task.DownloadProgress) error {
	_, err := e.UpdateTaskProgressEndpoint(ctx, &UpdateTaskProgressRequest{
		Id: id,
		Progress: &pb.DownloadProgress{
			TotalBytes: progress.TotalBytes,
		},
	})
	return err
}

func (e *Set) UpdateTaskError(ctx context.Context, id uint64, err error) error {
	_, err = e.UpdateTaskErrorEndpoint(ctx, &UpdateTaskErrorRequest{Id: id, Error: err.Error()})
	return err
}

func (e *Set) CompleteTask(ctx context.Context, id uint64) error {
	_, err := e.CompleteTaskEndpoint(ctx, &CompleteTaskRequest{Id: id})
	return err
}

func (e *Set) CheckFileExists(ctx context.Context, taskID uint64) (bool, error) {
	response, err := e.CheckFileExistsEndpoint(ctx, &CheckFileExistsRequest{TaskId: taskID})
	if err != nil {
		return false, err
	}
	resp := response.(*CheckFileExistsResponse)
	return resp.Exists, nil
}

func (e *Set) GetTaskProgress(ctx context.Context, taskID uint64) (*task.DownloadProgress, error) {
	response, err := e.GetTaskProgressEndpoint(ctx, &GetTaskProgressRequest{TaskId: taskID})
	if err != nil {
		return nil, err
	}
	resp := response.(*GetTaskProgressResponse)
	return fromPBDownloadProgress(resp.Progress), nil
}

func (e *Set) UpdateTaskChecksum(ctx context.Context, id uint64, checksum *task.ChecksumInfo) error {
	_, err := e.UpdateTaskChecksumEndpoint(ctx, &UpdateTaskChecksumRequest{
		TaskId:   id,
		Checksum: toPBChecksumInfo(checksum),
	})
	return err
}

func (e *Set) UpdateTaskMetadata(ctx context.Context, id uint64, metadata map[string]interface{}) error {
	_, err := e.UpdateTaskMetadataEndpoint(ctx, &UpdateTaskMetadataRequest{
		TaskId:   id,
		Metadata: toPBStruct(metadata),
	})
	return err
}

// fromPBTask converts a protobuf Task to domain Task
func fromPBTask(pbTask *pb.Task) *task.Task {
	if pbTask == nil {
		return nil
	}

	return &task.Task{
		ID:          pbTask.Id,
		OfAccountID: pbTask.OfAccountId,
		FileName:    pbTask.FileName,
		SourceURL:   pbTask.SourceUrl,
		SourceType:  task.SourceType(pbTask.SourceType),
		SourceAuth:  fromPBAuthConfig(pbTask.SourceAuth),
		StorageType: task.StorageType(pbTask.StorageType),
		StoragePath: pbTask.StoragePath,
		Checksum: &task.ChecksumInfo{
			ChecksumType:  pbTask.Checksum.ChecksumType,
			ChecksumValue: pbTask.Checksum.ChecksumValue,
		},
		Metadata:  pbTask.Metadata.AsMap(),
		CreatedAt: pbTask.CreatedAt.AsTime(),
		UpdatedAt: pbTask.UpdatedAt.AsTime(),
		CompletedAt: func() *time.Time {
			if pbTask.CompletedAt != nil {
				t := pbTask.CompletedAt.AsTime()
				return &t
			}
			return nil
		}(),
	}
}

func fromPBAuthConfig(pbAuth *pb.AuthConfig) *task.AuthConfig {
	if pbAuth == nil {
		return nil
	}
	return &task.AuthConfig{
		Username: pbAuth.Username,
		Password: pbAuth.Password,
		Token:    pbAuth.Token,
		Headers:  pbAuth.Headers,
	}
}

func toPBDownloadProgress(progress *task.DownloadProgress) *pb.DownloadProgress {
	if progress == nil {
		return nil
	}
	return &pb.DownloadProgress{
		TotalBytes: progress.TotalBytes,
	}
}

func fromPBDownloadProgress(pbProgress *pb.DownloadProgress) *task.DownloadProgress {
	if pbProgress == nil {
		return nil
	}
	return &task.DownloadProgress{
		TotalBytes: pbProgress.TotalBytes,
	}
}

func toPBChecksumInfo(checksum *task.ChecksumInfo) *pb.ChecksumInfo {
	if checksum == nil {
		return nil
	}
	return &pb.ChecksumInfo{
		ChecksumType:  checksum.ChecksumType,
		ChecksumValue: checksum.ChecksumValue,
	}
}

func fromPBChecksumInfo(pbChecksum *pb.ChecksumInfo) *task.ChecksumInfo {
	if pbChecksum == nil {
		return nil
	}
	return &task.ChecksumInfo{
		ChecksumType:  pbChecksum.ChecksumType,
		ChecksumValue: pbChecksum.ChecksumValue,
	}
}

// MakeCreateTaskEndpoint endpoint for Service.CreateTask
func MakeCreateTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateTaskRequest)

		params := &task.CreateTaskParam{
			OfAccountID: req.OfAccountId,
			FileName:    req.FileName,
			SourceURL:   req.SourceUrl,
			SourceType:  task.SourceType(req.SourceType.String()),
			SourceAuth: &task.AuthConfig{
				Username: req.SourceAuth.GetUsername(),
				Password: req.SourceAuth.GetPassword(),
				Token:    req.SourceAuth.GetToken(),
				Headers:  req.SourceAuth.GetHeaders(),
			},
			Checksum: &task.ChecksumInfo{
				ChecksumType:  req.Checksum.GetChecksumType(),
				ChecksumValue: req.Checksum.GetChecksumValue(),
			},
			Metadata: req.Metadata.AsMap(),
		}
		created, err := svc.CreateTask(ctx, params)
		if err != nil {
			return nil, err
		}
		return &TaskResponse{Task: toPBTask(created)}, nil
	}
}

// MakeGetTaskEndpoint endpoint for Service.GetTask
func MakeGetTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetTaskRequest)
		t, err := svc.GetTask(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		return &TaskResponse{Task: toPBTask(t)}, nil
	}
}

// MakeUpdateTaskStoragePathEndpoint updates storage path
func MakeUpdateTaskStoragePathEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*UpdateTaskStoragePathRequest)
		if err := svc.UpdateTaskStoragePath(ctx, req.Id, req.StoragePath); err != nil {
			return nil, err
		}
		t, err := svc.GetTask(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		return &TaskResponse{Task: toPBTask(t)}, nil
	}
}

// MakeUpdateTaskStatusEndpoint updates task status
func MakeUpdateTaskStatusEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*UpdateTaskStatusRequest)
		if err := svc.UpdateTaskStatus(ctx, req.Id, task.TaskStatus(req.Status.String())); err != nil {
			return nil, err
		}
		t, err := svc.GetTask(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		return &TaskResponse{Task: toPBTask(t)}, nil
	}
}

// MakeUpdateTaskProgressEndpoint updates progress
func MakeUpdateTaskProgressEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*UpdateTaskProgressRequest)
		if req.Progress != nil {
			if err := svc.UpdateTaskProgress(ctx, req.Id, toDomainProgress(req.Progress)); err != nil {
				return nil, err
			}
		}
		t, err := svc.GetTask(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		return &TaskResponse{Task: toPBTask(t)}, nil
	}
}

// MakeUpdateTaskErrorEndpoint updates error message
func MakeUpdateTaskErrorEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*UpdateTaskErrorRequest)
		if err := svc.UpdateTaskError(ctx, req.Id, stderrors.New(req.Error)); err != nil {
			return nil, err
		}
		t, err := svc.GetTask(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		return &TaskResponse{Task: toPBTask(t)}, nil
	}
}

// MakeCompleteTaskEndpoint marks task as completed with file info
func MakeCompleteTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CompleteTaskRequest)
		if err := svc.CompleteTask(ctx, req.Id); err != nil {
			return nil, err
		}
		t, err := svc.GetTask(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		return &TaskResponse{Task: toPBTask(t)}, nil
	}
}

// MakeListTasksEndpoint endpoint for Service.ListTasks
func MakeListTasksEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*ListTasksRequest)
		param := &task.ListTasksParam{
			Filter: &task.TaskFilter{
				OfAccountID: func() uint64 {
					if req.Filter != nil {
						return req.Filter.OfAccountId
					}
					return 0
				}(),
			},
			Offset:  req.Offset,
			Limit:   req.Limit,
			SortBy:  req.SortBy,
			SortAsc: req.SortAsc,
		}
		out, err := svc.ListTasks(ctx, param)
		if err != nil {
			return nil, err
		}
		resp := &ListTasksResponse{Total: out.Total}
		for _, t := range out.Tasks {
			resp.Tasks = append(resp.Tasks, toPBTask(t))
		}
		return resp, nil
	}
}

// MakeDeleteTaskEndpoint endpoint for Service.DeleteTask
func MakeDeleteTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*DeleteTaskRequest)
		if err := svc.DeleteTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &DeleteTaskResponse{Message: "deleted"}, nil
	}
}

// MakePauseTaskEndpoint endpoint for Service.PauseTask
func MakePauseTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*PauseTaskRequest)
		if err := svc.PauseTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &PauseTaskResponse{Message: "paused"}, nil
	}
}

// MakeResumeTaskEndpoint endpoint for Service.ResumeTask
func MakeResumeTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*ResumeTaskRequest)
		if err := svc.ResumeTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &ResumeTaskResponse{Message: "resumed"}, nil
	}
}

// MakeCancelTaskEndpoint endpoint for Service.CancelTask
func MakeCancelTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CancelTaskRequest)
		if err := svc.CancelTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &CancelTaskResponse{Message: "cancelled"}, nil
	}
}

// MakeRetryTaskEndpoint endpoint for Service.RetryTask
func MakeRetryTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*RetryTaskRequest)
		if err := svc.RetryTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &RetryTaskResponse{Message: "retried"}, nil
	}
}

// MakeCheckFileExistsEndpoint endpoint for Service.CheckFileExists
func MakeCheckFileExistsEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CheckFileExistsRequest)
		exists, err := svc.CheckFileExists(ctx, req.TaskId)
		if err != nil {
			return nil, err
		}
		return &CheckFileExistsResponse{Exists: exists}, nil
	}
}

// MakeGetTaskProgressEndpoint endpoint for Service.GetTaskProgress
func MakeGetTaskProgressEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetTaskProgressRequest)
		progress, err := svc.GetTaskProgress(ctx, req.TaskId)
		if err != nil {
			return nil, err
		}
		return &GetTaskProgressResponse{Progress: toPBDownloadProgress(progress)}, nil
	}
}

// MakeUpdateTaskChecksumEndpoint updates task checksum
func MakeUpdateTaskChecksumEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*UpdateTaskChecksumRequest)
		return nil, svc.UpdateTaskChecksum(ctx, req.TaskId, fromPBChecksumInfo(req.Checksum))
	}
}

// MakeUpdateTaskMetadataEndpoint updates task metadata
func MakeUpdateTaskMetadataEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*UpdateTaskMetadataRequest)
		return nil, svc.UpdateTaskMetadata(ctx, req.TaskId, fromPBStruct(req.Metadata))
	}
}

// New builds the Set with rate limiters similar to auth endpoints
func New(svc task.Service) Set {
	var (
		createEndpoint            endpoint.Endpoint
		getEndpoint               endpoint.Endpoint
		listEndpoint              endpoint.Endpoint
		deleteEndpoint            endpoint.Endpoint
		pauseEndpoint             endpoint.Endpoint
		resumeEndpoint            endpoint.Endpoint
		cancelEndpoint            endpoint.Endpoint
		retryEndpoint             endpoint.Endpoint
		updateStoragePathEndpoint endpoint.Endpoint
		updateStatusEndpoint      endpoint.Endpoint
		updateProgressEndpoint    endpoint.Endpoint
		updateErrorEndpoint       endpoint.Endpoint
		completeTaskEndpoint      endpoint.Endpoint
		checkFileExistsEndpoint   endpoint.Endpoint
		getTaskProgressEndpoint   endpoint.Endpoint
		updateChecksumEndpoint    endpoint.Endpoint
		updateMetadataEndpoint    endpoint.Endpoint
	)

	limiter := ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Limit(1), 100))

	createEndpoint = MakeCreateTaskEndpoint(svc)
	createEndpoint = limiter(createEndpoint)
	getEndpoint = MakeGetTaskEndpoint(svc)
	getEndpoint = limiter(getEndpoint)
	updateStoragePathEndpoint = MakeUpdateTaskStoragePathEndpoint(svc)
	updateStoragePathEndpoint = limiter(updateStoragePathEndpoint)
	updateStatusEndpoint = MakeUpdateTaskStatusEndpoint(svc)
	updateStatusEndpoint = limiter(updateStatusEndpoint)
	updateProgressEndpoint = MakeUpdateTaskProgressEndpoint(svc)
	updateProgressEndpoint = limiter(updateProgressEndpoint)
	updateErrorEndpoint = MakeUpdateTaskErrorEndpoint(svc)
	updateErrorEndpoint = limiter(updateErrorEndpoint)
	completeTaskEndpoint = MakeCompleteTaskEndpoint(svc)
	completeTaskEndpoint = limiter(completeTaskEndpoint)
	listEndpoint = MakeListTasksEndpoint(svc)
	listEndpoint = limiter(listEndpoint)
	deleteEndpoint = MakeDeleteTaskEndpoint(svc)
	deleteEndpoint = limiter(deleteEndpoint)
	pauseEndpoint = MakePauseTaskEndpoint(svc)
	pauseEndpoint = limiter(pauseEndpoint)
	resumeEndpoint = MakeResumeTaskEndpoint(svc)
	resumeEndpoint = limiter(resumeEndpoint)
	cancelEndpoint = MakeCancelTaskEndpoint(svc)
	cancelEndpoint = limiter(cancelEndpoint)
	retryEndpoint = MakeRetryTaskEndpoint(svc)
	retryEndpoint = limiter(retryEndpoint)
	checkFileExistsEndpoint = MakeCheckFileExistsEndpoint(svc)
	checkFileExistsEndpoint = limiter(checkFileExistsEndpoint)
	getTaskProgressEndpoint = MakeGetTaskProgressEndpoint(svc)
	getTaskProgressEndpoint = limiter(getTaskProgressEndpoint)
	updateChecksumEndpoint = MakeUpdateTaskChecksumEndpoint(svc)
	updateChecksumEndpoint = limiter(updateChecksumEndpoint)
	updateMetadataEndpoint = MakeUpdateTaskMetadataEndpoint(svc)
	updateMetadataEndpoint = limiter(updateMetadataEndpoint)

	return Set{
		CreateTaskEndpoint:            createEndpoint,
		GetTaskEndpoint:               getEndpoint,
		ListTasksEndpoint:             listEndpoint,
		DeleteTaskEndpoint:            deleteEndpoint,
		PauseTaskEndpoint:             pauseEndpoint,
		ResumeTaskEndpoint:            resumeEndpoint,
		CancelTaskEndpoint:            cancelEndpoint,
		RetryTaskEndpoint:             retryEndpoint,
		UpdateTaskStoragePathEndpoint: updateStoragePathEndpoint,
		UpdateTaskStatusEndpoint:      updateStatusEndpoint,
		UpdateTaskProgressEndpoint:    updateProgressEndpoint,
		UpdateTaskErrorEndpoint:       updateErrorEndpoint,
		CompleteTaskEndpoint:          completeTaskEndpoint,
		CheckFileExistsEndpoint:       checkFileExistsEndpoint,
		GetTaskProgressEndpoint:       getTaskProgressEndpoint,
		UpdateTaskChecksumEndpoint:    updateChecksumEndpoint,
		UpdateTaskMetadataEndpoint:    updateMetadataEndpoint,
	}
}

// Helper: minimal mapping to protobuf Task message
func toPBTask(t *task.Task) *pb.Task {
	if t == nil {
		return nil
	}
	return &pb.Task{
		Id:          t.ID,
		FileName:    t.FileName,
		SourceUrl:   t.SourceURL,
		SourceType:  pb.SourceType(pb.SourceType_value[string(t.SourceType)]),
		SourceAuth:  toPBAuthConfig(t.SourceAuth),
		StorageType: pb.StorageType(pb.StorageType_value[string(t.StorageType)]),
		StoragePath: t.StoragePath,
		Checksum: &pb.ChecksumInfo{
			ChecksumType:  t.Checksum.ChecksumType,
			ChecksumValue: t.Checksum.ChecksumValue,
		},
		Metadata:    toPBStruct(t.Metadata),
		OfAccountId: t.OfAccountID,
		CreatedAt:   timestamppb.New(t.CreatedAt),
		UpdatedAt:   timestamppb.New(t.UpdatedAt),
		CompletedAt: func() *timestamppb.Timestamp {
			if t.CompletedAt != nil {
				return timestamppb.New(*t.CompletedAt)
			}
			return nil
		}(),
	}
}

func toPBAuthConfig(auth *task.AuthConfig) *pb.AuthConfig {
	if auth == nil {
		return nil
	}
	return &pb.AuthConfig{
		Username: auth.Username,
		Password: auth.Password,
		Token:    auth.Token,
		Headers:  auth.Headers,
	}
}

func toPBDownloadOptions(options *task.DownloadOptions) *pb.DownloadOptions {
	if options == nil {
		return nil
	}
	return &pb.DownloadOptions{
		Concurrency: int32(options.Concurrency),
		MaxSpeed: func() int64 {
			if options.MaxSpeed != nil {
				return *options.MaxSpeed
			}
			return 0
		}(),
		MaxRetries: int32(options.MaxRetries),
		Timeout: func() int32 {
			if options.Timeout != nil {
				return int32(*options.Timeout)
			}
			return 0
		}(),
	}
}

func toPBStruct(metadata map[string]interface{}) *structpb.Struct {
	if metadata == nil {
		return nil
	}
	pbStruct, err := structpb.NewStruct(metadata)
	if err != nil {
		return nil
	}
	return pbStruct
}

func fromPBStruct(pbStruct *structpb.Struct) map[string]interface{} {
	if pbStruct == nil {
		return nil
	}
	return pbStruct.AsMap()
}

func toDomainProgress(p *pb.DownloadProgress) task.DownloadProgress {
	if p == nil {
		return task.DownloadProgress{}
	}
	return task.DownloadProgress{
		TotalBytes: p.TotalBytes,
	}
}

func toPBProgress(p *task.DownloadProgress) *pb.DownloadProgress {
	if p == nil {
		return nil
	}
	return &pb.DownloadProgress{
		TotalBytes: p.TotalBytes,
	}
}
