package taskendpoint

import (
	"context"
	stderrors "errors"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"

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

type StartTaskRequest pb.StartTaskRequest

type StartTaskResponse pb.StartTaskResponse

type PauseTaskRequest pb.PauseTaskRequest

type PauseTaskResponse pb.PauseTaskResponse

type ResumeTaskRequest pb.ResumeTaskRequest

type ResumeTaskResponse pb.ResumeTaskResponse

type CancelTaskRequest pb.CancelTaskRequest

type CancelTaskResponse pb.CancelTaskResponse

type RetryTaskRequest pb.RetryTaskRequest

type RetryTaskResponse pb.RetryTaskResponse

type GetFileInfoRequest pb.GetFileInfoRequest

type GetFileInfoResponse pb.GetFileInfoResponse

type CheckFileExistsRequest pb.CheckFileExistsRequest

type CheckFileExistsResponse pb.CheckFileExistsResponse

type GetTaskProgressRequest pb.GetTaskProgressRequest

type GetTaskProgressResponse pb.GetTaskProgressResponse

// Set contains all endpoints for the Task Service

type Set struct {
	CreateTaskEndpoint            endpoint.Endpoint
	GetTaskEndpoint               endpoint.Endpoint
	ListTasksEndpoint             endpoint.Endpoint
	DeleteTaskEndpoint            endpoint.Endpoint
	StartTaskEndpoint             endpoint.Endpoint
	PauseTaskEndpoint             endpoint.Endpoint
	ResumeTaskEndpoint            endpoint.Endpoint
	CancelTaskEndpoint            endpoint.Endpoint
	RetryTaskEndpoint             endpoint.Endpoint
	UpdateTaskStoragePathEndpoint endpoint.Endpoint
	UpdateTaskStatusEndpoint      endpoint.Endpoint
	UpdateTaskProgressEndpoint    endpoint.Endpoint
	UpdateTaskErrorEndpoint       endpoint.Endpoint
	CompleteTaskEndpoint          endpoint.Endpoint
	GetFileInfoEndpoint           endpoint.Endpoint
	CheckFileExistsEndpoint       endpoint.Endpoint
	GetTaskProgressEndpoint       endpoint.Endpoint
}

// Implement task.Service on the Set for GRPC client usage

func (e *Set) CreateTask(ctx context.Context, param *task.CreateTaskParam) (*task.Task, error) {
	resp, err := e.CreateTaskEndpoint(ctx, &CreateTaskRequest{
		OfAccountId: param.OfAccountID,
		Name:        param.Name,
		Description: param.Description,
		SourceUrl:   param.SourceURL,
		SourceType:  pb.SourceType(pb.SourceType_value[string(param.SourceType)]),
		SourceAuth:  toPBAuthConfig(param.SourceAuth),
		Options:     toPBDownloadOptions(param.Options),
		MaxRetries:  param.MaxRetries,
		Tags:        param.Tags,
		Metadata:    toPBStruct(param.Metadata),
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

// fromPBTask converts a protobuf Task to domain Task
func fromPBTask(pbTask *pb.Task) *task.Task {
	if pbTask == nil {
		return nil
	}

	return &task.Task{
		ID:          pbTask.Id,
		OfAccountID: pbTask.OfAccountId,
		Name:        pbTask.Name,
		Description: pbTask.Description,
		SourceURL:   pbTask.SourceUrl,
		SourceType:  task.SourceType(pbTask.SourceType),
		SourceAuth:  fromPBAuthConfig(pbTask.SourceAuth),
		StorageType: task.StorageType(pbTask.StorageType),
		StoragePath: pbTask.StoragePath,
		Status:      task.TaskStatus(pbTask.Status),
		FileInfo:    fromPBFileInfo(pbTask.FileInfo),
		Progress:    fromPBDownloadProgress(pbTask.Progress),
		Options:     fromPBDownloadOptions(pbTask.Options),
		CreatedAt:   pbTask.CreatedAt.AsTime(),
		UpdatedAt:   pbTask.UpdatedAt.AsTime(),
		CompletedAt: func() *time.Time {
			if pbTask.CompletedAt != nil {
				t := pbTask.CompletedAt.AsTime()
				return &t
			}
			return nil
		}(),
		Error:      pbTask.Error,
		RetryCount: pbTask.RetryCount,
		MaxRetries: pbTask.MaxRetries,
		Tags:       pbTask.Tags,
		Metadata:   pbTask.Metadata.AsMap(),
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
func fromPBFileInfo(pbFileInfo *pb.FileInfo) *task.FileInfo {
	if pbFileInfo == nil {
		return nil
	}
	return &task.FileInfo{
		FileName:    pbFileInfo.FileName,
		FileSize:    pbFileInfo.FileSize,
		ContentType: pbFileInfo.ContentType,
		MD5Hash:     pbFileInfo.Md5Hash,
		StorageKey:  pbFileInfo.StorageKey,
		StoredAt:    pbFileInfo.StoredAt.AsTime(),
	}
}
func fromPBDownloadProgress(pbProgress *pb.DownloadProgress) *task.DownloadProgress {
	if pbProgress == nil {
		return nil
	}
	return &task.DownloadProgress{
		BytesDownloaded: pbProgress.BytesDownloaded,
		TotalBytes:      pbProgress.TotalBytes,
		Speed:           pbProgress.SpeedBps,
		ETA:             time.Duration(pbProgress.EtaSeconds) * time.Second,
		Percentage:      pbProgress.Percentage,
	}
}
func fromPBDownloadOptions(pbOptions *pb.DownloadOptions) *task.DownloadOptions {
	if pbOptions == nil {
		return nil
	}
	return &task.DownloadOptions{
		ChunkSize:    pbOptions.ChunkSize,
		MaxRetries:   int(pbOptions.MaxRetries),
		Timeout:      time.Duration(pbOptions.TimeoutSeconds) * time.Second,
		Resume:       pbOptions.Resume,
		ChecksumType: pbOptions.ChecksumType,
	}
}

func (e *Set) StartTask(ctx context.Context, taskID uint64) error {
	_, err := e.StartTaskEndpoint(ctx, &StartTaskRequest{Id: taskID})
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
	_, err := e.UpdateTaskProgressEndpoint(ctx, &UpdateTaskProgressRequest{Id: id, Progress: &pb.DownloadProgress{BytesDownloaded: progress.BytesDownloaded, TotalBytes: progress.TotalBytes, SpeedBps: progress.Speed}})
	return err
}
func (e *Set) UpdateTaskError(ctx context.Context, id uint64, err error) error {
	_, err = e.UpdateTaskErrorEndpoint(ctx, &UpdateTaskErrorRequest{Id: id, Error: err.Error()})
	return err
}
func (e *Set) CompleteTask(ctx context.Context, id uint64, fileInfo *task.FileInfo) error {
	_, err := e.CompleteTaskEndpoint(ctx, &CompleteTaskRequest{Id: id, FileInfo: &pb.FileInfo{FileName: fileInfo.FileName, FileSize: fileInfo.FileSize, ContentType: fileInfo.ContentType, Md5Hash: fileInfo.MD5Hash, StorageKey: fileInfo.StorageKey}})
	return err
}
func (e *Set) GetFileInfo(ctx context.Context, taskID uint64) (*task.FileInfo, error) {
	resp, err := e.GetFileInfoEndpoint(ctx, &GetFileInfoRequest{TaskId: taskID})
	if err != nil {
		return nil, err
	}
	out := resp.(*GetFileInfoResponse)
	return toDomainFileInfo(out.FileInfo), nil
}
func (e *Set) CheckFileExists(ctx context.Context, taskID uint64) (bool, error) {
	resp, err := e.CheckFileExistsEndpoint(ctx, &CheckFileExistsRequest{TaskId: taskID})
	if err != nil {
		return false, err
	}
	out := resp.(*CheckFileExistsResponse)
	return out.Exists, nil
}
func (e *Set) GetTaskProgress(ctx context.Context, taskID uint64) (*task.DownloadProgress, error) {
	resp, err := e.GetTaskProgressEndpoint(ctx, &GetTaskProgressRequest{TaskId: taskID})
	if err != nil {
		return nil, err
	}
	out := resp.(*GetTaskProgressResponse)
	if out.Progress == nil {
		return nil, nil
	}
	p := toDomainProgress(out.Progress)
	return &p, nil
}

// MakeCreateTaskEndpoint endpoint for Service.CreateTask
func MakeCreateTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateTaskRequest)

		params := &task.CreateTaskParam{
			OfAccountID: req.OfAccountId,
			Name:        req.Name,
			Description: req.Description,
			SourceURL:   req.SourceUrl,
			SourceType:  task.SourceType(req.SourceType.String()),
			SourceAuth: &task.AuthConfig{
				Username: req.SourceAuth.GetUsername(),
				Password: req.SourceAuth.GetPassword(),
				Token:    req.SourceAuth.GetToken(),
				Headers:  req.SourceAuth.GetHeaders(),
			},
			Options: &task.DownloadOptions{
				ChunkSize:    req.Options.GetChunkSize(),
				MaxRetries:   int(req.Options.GetMaxRetries()),
				Timeout:      time.Duration(req.Options.GetTimeoutSeconds()) * time.Second,
				Resume:       req.Options.GetResume(),
				ChecksumType: req.Options.GetChecksumType(),
			},
			MaxRetries: req.MaxRetries,
			Tags:       req.Tags,
			Metadata:   req.Metadata.AsMap(),
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
		if err := svc.CompleteTask(ctx, req.Id, toDomainFileInfo(req.FileInfo)); err != nil {
			return nil, err
		}
		t, err := svc.GetTask(ctx, req.Id)
		if err != nil {
			return nil, err
		}
		return &TaskResponse{Task: toPBTask(t)}, nil
	}
}

// MakeGetFileInfoEndpoint retrieves file info for a task
func MakeGetFileInfoEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetFileInfoRequest)
		fi, err := svc.GetFileInfo(ctx, req.TaskId)
		if err != nil {
			return nil, err
		}
		return &GetFileInfoResponse{FileInfo: toPBFileInfo(fi)}, nil
	}
}

// MakeCheckFileExistsEndpoint checks if a file exists for a task
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

// MakeGetTaskProgressEndpoint retrieves progress for a task
func MakeGetTaskProgressEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetTaskProgressRequest)
		prog, err := svc.GetTaskProgress(ctx, req.TaskId)
		if err != nil {
			return nil, err
		}
		return &GetTaskProgressResponse{Progress: toPBProgress(prog)}, nil
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

// MakeStartTaskEndpoint endpoint for Service.StartTask
func MakeStartTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*StartTaskRequest)
		if err := svc.StartTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &StartTaskResponse{Message: "started"}, nil
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

// New builds the Set with rate limiters similar to auth endpoints
func New(svc task.Service) Set {
	var (
		createEndpoint            endpoint.Endpoint
		getEndpoint               endpoint.Endpoint
		listEndpoint              endpoint.Endpoint
		deleteEndpoint            endpoint.Endpoint
		startEndpoint             endpoint.Endpoint
		pauseEndpoint             endpoint.Endpoint
		resumeEndpoint            endpoint.Endpoint
		cancelEndpoint            endpoint.Endpoint
		retryEndpoint             endpoint.Endpoint
		updateStoragePathEndpoint endpoint.Endpoint
		updateStatusEndpoint      endpoint.Endpoint
		updateProgressEndpoint    endpoint.Endpoint
		updateErrorEndpoint       endpoint.Endpoint
		completeTaskEndpoint      endpoint.Endpoint
		getFileInfoEndpoint       endpoint.Endpoint
		checkFileExistsEndpoint   endpoint.Endpoint
		getTaskProgressEndpoint   endpoint.Endpoint
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
	startEndpoint = MakeStartTaskEndpoint(svc)
	startEndpoint = limiter(startEndpoint)
	pauseEndpoint = MakePauseTaskEndpoint(svc)
	pauseEndpoint = limiter(pauseEndpoint)
	resumeEndpoint = MakeResumeTaskEndpoint(svc)
	resumeEndpoint = limiter(resumeEndpoint)
	cancelEndpoint = MakeCancelTaskEndpoint(svc)
	cancelEndpoint = limiter(cancelEndpoint)
	retryEndpoint = MakeRetryTaskEndpoint(svc)
	retryEndpoint = limiter(retryEndpoint)
	getFileInfoEndpoint = MakeGetFileInfoEndpoint(svc)
	getFileInfoEndpoint = limiter(getFileInfoEndpoint)
	checkFileExistsEndpoint = MakeCheckFileExistsEndpoint(svc)
	checkFileExistsEndpoint = limiter(checkFileExistsEndpoint)
	getTaskProgressEndpoint = MakeGetTaskProgressEndpoint(svc)
	getTaskProgressEndpoint = limiter(getTaskProgressEndpoint)

	return Set{
		CreateTaskEndpoint:            createEndpoint,
		GetTaskEndpoint:               getEndpoint,
		ListTasksEndpoint:             listEndpoint,
		DeleteTaskEndpoint:            deleteEndpoint,
		StartTaskEndpoint:             startEndpoint,
		PauseTaskEndpoint:             pauseEndpoint,
		ResumeTaskEndpoint:            resumeEndpoint,
		CancelTaskEndpoint:            cancelEndpoint,
		RetryTaskEndpoint:             retryEndpoint,
		UpdateTaskStoragePathEndpoint: updateStoragePathEndpoint,
		UpdateTaskStatusEndpoint:      updateStatusEndpoint,
		UpdateTaskProgressEndpoint:    updateProgressEndpoint,
		UpdateTaskErrorEndpoint:       updateErrorEndpoint,
		CompleteTaskEndpoint:          completeTaskEndpoint,
		GetFileInfoEndpoint:           getFileInfoEndpoint,
		CheckFileExistsEndpoint:       checkFileExistsEndpoint,
		GetTaskProgressEndpoint:       getTaskProgressEndpoint,
	}
}

// Helper: minimal mapping to protobuf Task message
func toPBTask(t *task.Task) *pb.Task {
	if t == nil {
		return nil
	}
	return &pb.Task{
		Id:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		SourceUrl:   t.SourceURL,
		SourceType:  pb.SourceType(pb.SourceType_value[string(t.SourceType)]),
		SourceAuth:  toPBAuthConfig(t.SourceAuth),
		StorageType: pb.StorageType(pb.StorageType_value[string(t.StorageType)]),
		StoragePath: t.StoragePath,
		Status:      pb.TaskStatus(pb.TaskStatus_value[string(t.Status)]),
		FileInfo:    toPBFileInfo(t.FileInfo),
		Progress:    toPBProgress(t.Progress),
		Options:     toPBDownloadOptions(t.Options),
		CreatedAt:   timestamppb.New(t.CreatedAt),
		UpdatedAt:   timestamppb.New(t.UpdatedAt),
		CompletedAt: func() *timestamppb.Timestamp {
			if t.CompletedAt != nil {
				return timestamppb.New(*t.CompletedAt)
			}
			return nil
		}(),
		Error:       t.Error,
		RetryCount:  t.RetryCount,
		MaxRetries:  t.MaxRetries,
		Tags:        t.Tags,
		Metadata:    toPBStruct(t.Metadata),
		OfAccountId: t.OfAccountID,
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
		ChunkSize:      options.ChunkSize,
		MaxRetries:     int32(options.MaxRetries),
		TimeoutSeconds: int64(options.Timeout.Seconds()),
		Resume:         options.Resume,
		ChecksumType:   options.ChecksumType,
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

func toDomainProgress(p *pb.DownloadProgress) task.DownloadProgress {
	if p == nil {
		return task.DownloadProgress{}
	}
	return task.DownloadProgress{
		BytesDownloaded: p.BytesDownloaded,
		TotalBytes:      p.TotalBytes,
		Speed:           p.SpeedBps,
		ETA:             time.Duration(p.EtaSeconds) * time.Second,
		Percentage:      p.Percentage,
	}
}

func toDomainFileInfo(f *pb.FileInfo) *task.FileInfo {
	if f == nil {
		return nil
	}
	return &task.FileInfo{
		FileName:    f.FileName,
		FileSize:    f.FileSize,
		ContentType: f.ContentType,
		MD5Hash:     f.Md5Hash,
		StorageKey:  f.StorageKey,
		StoredAt:    f.StoredAt.AsTime(),
	}
}

func toPBFileInfo(f *task.FileInfo) *pb.FileInfo {
	if f == nil {
		return nil
	}
	return &pb.FileInfo{
		FileName:    f.FileName,
		FileSize:    f.FileSize,
		ContentType: f.ContentType,
		Md5Hash:     f.MD5Hash,
		StorageKey:  f.StorageKey,
		StoredAt:    timestamppb.New(f.StoredAt),
	}
}

func toPBProgress(p *task.DownloadProgress) *pb.DownloadProgress {
	if p == nil {
		return nil
	}
	return &pb.DownloadProgress{
		BytesDownloaded: p.BytesDownloaded,
		TotalBytes:      p.TotalBytes,
		SpeedBps:        p.Speed,
		EtaSeconds:      int64(p.ETA.Seconds()),
		Percentage:      p.Percentage,
	}
}
