package apigateway

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/samber/lo"
	"github.com/yuisofull/goload/internal/apigateway/gen"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/task"
)

type ListTasksRequest = gen.ListTasksParams

type ListTasksResponse = gen.ListTasksResponse

type Task = gen.Task

// taskToAPI maps domain task to API Task response used by endpoints.
func taskToAPI(t *task.Task) *Task {
	if t == nil {
		return nil
	}

	var checksumType, checksumValue *string
	if t.Checksum != nil {
		checksumType = &t.Checksum.ChecksumType
		checksumValue = &t.Checksum.ChecksumValue
	}

	var progress *float64
	var downloadedBytes, totalBytes *int64
	if t.Progress != nil {
		progress = &t.Progress.Progress
		downloadedBytes = &t.Progress.DownloadedBytes
		totalBytes = &t.Progress.TotalBytes
	}

	return &Task{
		Id:              &t.ID,
		OfAccountId:     &t.OfAccountID,
		FileName:        &t.FileName,
		SourceUrl:       &t.SourceURL,
		SourceType:      lo.ToPtr(t.SourceType.String()),
		ChecksumType:    checksumType,
		ChecksumValue:   checksumValue,
		Status:          lo.ToPtr(t.Status.String()),
		Progress:        lo.ToPtr(float32(lo.FromPtr(progress))),
		DownloadedBytes: downloadedBytes,
		TotalBytes:      totalBytes,
		ErrorMessage:    t.ErrorMessage,
		Metadata:        lo.ToPtr(t.Metadata),
		CreatedAt:       &t.CreatedAt,
		UpdatedAt:       &t.UpdatedAt,
		CompletedAt:     t.CompletedAt,
	}
}

type GatewayEndpoints struct {
	CreateTaskEndpoint          endpoint.Endpoint
	GetTaskEndpoint             endpoint.Endpoint
	ListTasksEndpoint           endpoint.Endpoint
	DeleteTaskEndpoint          endpoint.Endpoint
	PauseTaskEndpoint           endpoint.Endpoint
	ResumeTaskEndpoint          endpoint.Endpoint
	CancelTaskEndpoint          endpoint.Endpoint
	RetryTaskEndpoint           endpoint.Endpoint
	CheckFileExistsEndpoint     endpoint.Endpoint
	GetTaskProgressEndpoint     endpoint.Endpoint
	GenerateDownloadURLEndpoint endpoint.Endpoint
	// Auth endpoints (public)
	AuthCreateEndpoint  endpoint.Endpoint
	AuthSessionEndpoint endpoint.Endpoint
}

type CreateTaskRequest = gen.CreateTaskRequest

type CreateTaskResponse = gen.CreateTaskResponse

type GetTaskRequest = gen.GetTaskParams

type GetTaskResponse = gen.GetTaskResponse

type DeleteTaskRequest = gen.DeleteTaskParams
type DeleteTaskResponse = gen.SuccessResponse

type PauseTaskRequest = gen.PauseTaskParams
type PauseTaskResponse = gen.SuccessResponse

type ResumeTaskRequest = gen.ResumeTaskParams
type ResumeTaskResponse = gen.SuccessResponse

type CancelTaskRequest = gen.CancelTaskParams
type CancelTaskResponse = gen.SuccessResponse

type RetryTaskRequest = gen.RetryTaskParams
type RetryTaskResponse = gen.SuccessResponse

type CheckFileExistsRequest = gen.CheckFileExistsParams
type CheckFileExistsResponse = gen.CheckFileExistsResponse

type GetTaskProgressRequest = gen.GetTaskProgressParams
type GetTaskProgressResponse = gen.GetTaskProgressResponse

type GenerateDownloadURLRequest = gen.GenerateDownloadURLRequest

type GenerateDownloadURLResponse = gen.GenerateDownloadURLResponse

// MakeGenerateDownloadURLEndpoint calls the task service GenerateDownloadURL,
// which internally handles presigning (direct MinIO URL) or token fallback.
func MakeGenerateDownloadURLEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GenerateDownloadURLRequest)

		userID, ok := UserIDFromContext(ctx)
		if !ok {
			return nil, &errors.Error{Code: errors.ErrCodeUnauthenticated, Message: "unauthenticated"}
		}

		t, err := svc.GetTask(ctx, req.TaskId)
		if err != nil {
			return nil, err
		}
		if t.OfAccountID != userID {
			return nil, &errors.Error{Code: errors.ErrCodePermissionDenied, Message: "forbidden"}
		}

		ttl := time.Duration(req.TtlSeconds) * time.Second
		if ttl <= 0 {
			ttl = time.Hour
		}

		urlStr, direct, err := svc.GenerateDownloadURL(ctx, req.TaskId, ttl, req.OneTime)
		if err != nil {
			return nil, err
		}
		return &GenerateDownloadURLResponse{Url: &urlStr, Direct: &direct}, nil
	}
}

func MakeListTasksEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*ListTasksRequest)

		// Ensure user is authenticated. The auth middleware should have
		// populated the account ID into the context by the time this
		// endpoint executes.
		userID, ok := UserIDFromContext(ctx)
		if !ok {
			return nil, &errors.Error{Code: errors.ErrCodeUnauthenticated, Message: "unauthenticated"}
		}

		var offset, limit uint64
		if req.Offset != nil {
			offset = *req.Offset
		}
		if req.Limit != nil {
			limit = *req.Limit
		}

		params := task.ListTasksParam{
			Filter: &task.TaskFilter{
				OfAccountID: userID,
			},
			Offset: int32(offset),
			Limit:  int32(limit),
		}

		result, err := svc.ListTasks(ctx, &params)
		if err != nil {
			return nil, err
		}

		var count int32 = result.Total
		tasks := lo.Map(result.Tasks, func(t *task.Task, _ int) Task { return *taskToAPI(t) })
		return &ListTasksResponse{
			Tasks:      &tasks,
			TotalCount: &count,
		}, nil
	}
}

// MakeCreateTaskEndpoint allows creating a new task. The endpoint enforces that the
// authenticated user (from context) is set as the task owner.
func MakeCreateTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateTaskRequest)

		userID, ok := UserIDFromContext(ctx)
		if !ok {
			return nil, &errors.Error{Code: errors.ErrCodeUnauthenticated, Message: "unauthenticated"}
		}

		var metadata map[string]any
		if req.Metadata != nil {
			metadata = *req.Metadata
		}

		param := task.CreateTaskParam{
			OfAccountID: userID,
			FileName:    req.FileName,
			SourceURL:   req.SourceUrl,
			SourceType:  task.ToSourceType(req.SourceType),
			Metadata:    metadata,
		}

		if param.SourceType == task.SourceBitTorrent && metadata != nil {
			if raw, ok := metadata["torrent_file_base64"]; ok {
				if s, ok := raw.(string); ok && s != "" {
					param.SourceURL = fmt.Sprintf("data:application/x-bittorrent;base64,%s", s)
				}
				delete(metadata, "torrent_file_base64")
			}
		}
		if req.ChecksumType != nil || req.ChecksumValue != nil {
			var ctype, cval string
			if req.ChecksumType != nil {
				ctype = *req.ChecksumType
			}
			if req.ChecksumValue != nil {
				cval = *req.ChecksumValue
			}
			param.Checksum = &task.ChecksumInfo{ChecksumType: ctype, ChecksumValue: cval}
		}

		t, err := svc.CreateTask(ctx, &param)
		if err != nil {
			return nil, err
		}

		return &CreateTaskResponse{Task: taskToAPI(t)}, nil
	}
}

func MakeGetTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetTaskRequest)
		t, err := svc.GetTask(ctx, req.Id)
		if err != nil {
			return nil, err
		}

		return &GetTaskResponse{Task: taskToAPI(t)}, nil
	}
}

type CreateAccountGatewayRequest = gen.CreateAccountGatewayRequest

type CreateAccountGatewayResponse = gen.CreateAccountGatewayResponse

type CreateSessionGatewayRequest = gen.CreateSessionGatewayRequest

type CreateSessionGatewayResponse = gen.CreateSessionGatewayResponse

type AuthAccount = gen.AuthAccount

func MakeCreateAccountEndpoint(svc auth.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateAccountGatewayRequest)
		out, err := svc.CreateAccount(
			ctx,
			auth.CreateAccountParams{AccountName: req.AccountName, Password: req.Password},
		)
		if err != nil {
			return nil, err
		}
		return &CreateAccountGatewayResponse{Id: &out.ID, AccountName: &out.AccountName}, nil
	}
}

func MakeCreateSessionEndpoint(svc auth.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateSessionGatewayRequest)
		out, err := svc.CreateSession(
			ctx,
			auth.CreateSessionParams{AccountName: req.AccountName, Password: req.Password},
		)
		if err != nil {
			return nil, err
		}
		var acct *AuthAccount
		if out.Account != nil {
			acct = &AuthAccount{Id: &out.Account.Id, AccountName: &out.Account.AccountName}
		}
		return &CreateSessionGatewayResponse{Token: &out.Token, Account: acct}, nil
	}
}

func MakeDeleteTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*DeleteTaskRequest)
		if err := svc.DeleteTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &DeleteTaskResponse{Success: lo.ToPtr(true)}, nil
	}
}

func MakePauseTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*PauseTaskRequest)
		if err := svc.PauseTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &PauseTaskResponse{Success: lo.ToPtr(true)}, nil
	}
}

func MakeResumeTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*ResumeTaskRequest)
		if err := svc.ResumeTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &ResumeTaskResponse{Success: lo.ToPtr(true)}, nil
	}
}

func MakeCancelTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CancelTaskRequest)
		if err := svc.CancelTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &CancelTaskResponse{Success: lo.ToPtr(true)}, nil
	}
}

func MakeRetryTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*RetryTaskRequest)
		if err := svc.RetryTask(ctx, req.Id); err != nil {
			return nil, err
		}
		return &RetryTaskResponse{Success: lo.ToPtr(true)}, nil
	}
}

func MakeCheckFileExistsEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CheckFileExistsRequest)
		okExists, err := svc.CheckFileExists(ctx, req.TaskId)
		if err != nil {
			return nil, err
		}
		return &CheckFileExistsResponse{Exists: &okExists}, nil
	}
}

func MakeGetTaskProgressEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetTaskProgressRequest)

		progress, err := svc.GetTaskProgress(ctx, req.TaskId)
		if err != nil {
			return nil, err
		}

		var p *float32
		var d, tot *int64
		if progress != nil {
			pf := float32(progress.Progress)
			p = &pf
			d = &progress.DownloadedBytes
			tot = &progress.TotalBytes
		}

		return &GetTaskProgressResponse{Progress: p, DownloadedBytes: d, TotalBytes: tot}, nil
	}
}

func NewGatewayEndpoints(
	downloadTaskSvc task.Service,
	authMW endpoint.Middleware,
	authSvc auth.Service,
) GatewayEndpoints {
	var authCreate endpoint.Endpoint
	var authSession endpoint.Endpoint
	if authSvc != nil {
		authCreate = MakeCreateAccountEndpoint(authSvc)
		authSession = MakeCreateSessionEndpoint(authSvc)
	}

	return GatewayEndpoints{
		CreateTaskEndpoint: authMW(MakeCreateTaskEndpoint(downloadTaskSvc)),
		GetTaskEndpoint: authMW(
			RequireTaskOwnerMiddleware(
				downloadTaskSvc,
				func(req interface{}) uint64 { return req.(*GetTaskRequest).Id },
			)(
				MakeGetTaskEndpoint(downloadTaskSvc),
			),
		),
		ListTasksEndpoint: authMW(MakeListTasksEndpoint(downloadTaskSvc)),
		DeleteTaskEndpoint: authMW(
			RequireTaskOwnerMiddleware(
				downloadTaskSvc,
				func(req interface{}) uint64 { return req.(*DeleteTaskRequest).Id },
			)(
				MakeDeleteTaskEndpoint(downloadTaskSvc),
			),
		),
		PauseTaskEndpoint: authMW(
			RequireTaskOwnerMiddleware(
				downloadTaskSvc,
				func(req interface{}) uint64 { return req.(*PauseTaskRequest).Id },
			)(
				MakePauseTaskEndpoint(downloadTaskSvc),
			),
		),
		ResumeTaskEndpoint: authMW(
			RequireTaskOwnerMiddleware(
				downloadTaskSvc,
				func(req interface{}) uint64 { return req.(*ResumeTaskRequest).Id },
			)(
				MakeResumeTaskEndpoint(downloadTaskSvc),
			),
		),
		CancelTaskEndpoint: authMW(
			RequireTaskOwnerMiddleware(
				downloadTaskSvc,
				func(req interface{}) uint64 { return req.(*CancelTaskRequest).Id },
			)(
				MakeCancelTaskEndpoint(downloadTaskSvc),
			),
		),
		RetryTaskEndpoint: authMW(
			RequireTaskOwnerMiddleware(
				downloadTaskSvc,
				func(req interface{}) uint64 { return req.(*RetryTaskRequest).Id },
			)(
				MakeRetryTaskEndpoint(downloadTaskSvc),
			),
		),
		CheckFileExistsEndpoint: authMW(
			RequireTaskOwnerMiddleware(
				downloadTaskSvc,
				func(req interface{}) uint64 { return req.(*CheckFileExistsRequest).TaskId },
			)(
				MakeCheckFileExistsEndpoint(downloadTaskSvc),
			),
		),
		GetTaskProgressEndpoint: authMW(
			RequireTaskOwnerMiddleware(
				downloadTaskSvc,
				func(req interface{}) uint64 { return req.(*GetTaskProgressRequest).TaskId },
			)(
				MakeGetTaskProgressEndpoint(downloadTaskSvc),
			),
		),
		GenerateDownloadURLEndpoint: authMW(
			RequireTaskOwnerMiddleware(
				downloadTaskSvc,
				func(req interface{}) uint64 { return req.(*GenerateDownloadURLRequest).TaskId },
			)(
				MakeGenerateDownloadURLEndpoint(downloadTaskSvc),
			),
		),
		AuthCreateEndpoint:  authCreate,
		AuthSessionEndpoint: authSession,
	}
}
