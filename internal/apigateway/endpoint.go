package apigateway

import (
	"context"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/samber/lo"
	"github.com/yuisofull/goload/internal/auth"
	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/task"
)

type ListTasksRequest struct {
	Filter *struct {
		OfAccountID uint64
	}
	Offset int32
	Limit  int32
}

type ListTasksResponse struct {
	Tasks      []*Task `json:"tasks"`
	TotalCount int32   `json:"total_count"`
}

type Task struct {
	ID              uint64         `json:"id"`
	OfAccountID     uint64         `json:"of_account_id"`
	FileName        string         `json:"file_name"`
	SourceUrl       string         `json:"source_url"`
	SourceType      string         `json:"source_type"`
	ChecksumType    *string        `json:"checksum_type"`
	ChecksumValue   *string        `json:"checksum_value"`
	Status          string         `json:"status"`
	Progress        *float64       `json:"progress"`
	DownloadedBytes *int64         `json:"downloaded_bytes"`
	TotalBytes      *int64         `json:"total_bytes"`
	ErrorMessage    *string        `json:"error_message"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       *time.Time     `json:"created_at"`
	UpdatedAt       *time.Time     `json:"updated_at"`
	CompletedAt     *time.Time     `json:"completed_at"`
}

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
		ID:              t.ID,
		OfAccountID:     t.OfAccountID,
		FileName:        t.FileName,
		SourceUrl:       t.SourceURL,
		SourceType:      t.SourceType.String(),
		ChecksumType:    checksumType,
		ChecksumValue:   checksumValue,
		Status:          t.Status.String(),
		Progress:        progress,
		DownloadedBytes: downloadedBytes,
		TotalBytes:      totalBytes,
		ErrorMessage:    t.ErrorMessage,
		Metadata:        t.Metadata,
		CreatedAt:       &t.CreatedAt,
		UpdatedAt:       &t.UpdatedAt,
		CompletedAt:     t.CompletedAt,
	}
}

type GatewayEndpoints struct {
	CreateTaskEndpoint      endpoint.Endpoint
	GetTaskEndpoint         endpoint.Endpoint
	ListTasksEndpoint       endpoint.Endpoint
	DeleteTaskEndpoint      endpoint.Endpoint
	PauseTaskEndpoint       endpoint.Endpoint
	ResumeTaskEndpoint      endpoint.Endpoint
	CancelTaskEndpoint      endpoint.Endpoint
	RetryTaskEndpoint       endpoint.Endpoint
	CheckFileExistsEndpoint endpoint.Endpoint
	GetTaskProgressEndpoint endpoint.Endpoint
	// Auth endpoints (public)
	AuthCreateEndpoint  endpoint.Endpoint
	AuthSessionEndpoint endpoint.Endpoint
}

type CreateTaskRequest struct {
	FileName      string         `json:"file_name"`
	SourceUrl     string         `json:"source_url"`
	SourceType    string         `json:"source_type"`
	ChecksumType  *string        `json:"checksum_type,omitempty"`
	ChecksumValue *string        `json:"checksum_value,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type CreateTaskResponse struct {
	Task *Task `json:"task"`
}

type GetTaskRequest struct {
	ID uint64 `json:"id"`
}

type GetTaskResponse struct {
	Task *Task `json:"task"`
}

type (
	DeleteTaskRequest  struct{ ID uint64 }
	DeleteTaskResponse struct{ Success bool }
)

type (
	PauseTaskRequest  struct{ ID uint64 }
	PauseTaskResponse struct{ Success bool }
)

type (
	ResumeTaskRequest  struct{ ID uint64 }
	ResumeTaskResponse struct{ Success bool }
)

type (
	CancelTaskRequest  struct{ ID uint64 }
	CancelTaskResponse struct{ Success bool }
)

type (
	RetryTaskRequest  struct{ ID uint64 }
	RetryTaskResponse struct{ Success bool }
)

type (
	CheckFileExistsRequest  struct{ TaskId uint64 }
	CheckFileExistsResponse struct{ Exists bool }
)

type (
	GetTaskProgressRequest  struct{ TaskId uint64 }
	GetTaskProgressResponse struct {
		Progress        *float64 `json:"progress,omitempty"`
		DownloadedBytes *int64   `json:"downloaded_bytes,omitempty"`
		TotalBytes      *int64   `json:"total_bytes,omitempty"`
	}
)

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

		params := task.ListTasksParam{
			Filter: &task.TaskFilter{
				OfAccountID: userID,
			},
			Offset: req.Offset,
			Limit:  req.Limit,
		}

		result, err := svc.ListTasks(ctx, &params)
		if err != nil {
			return nil, err
		}

		return &ListTasksResponse{
			Tasks:      lo.Map(result.Tasks, func(t *task.Task, _ int) *Task { return taskToAPI(t) }),
			TotalCount: result.Total,
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

		param := task.CreateTaskParam{
			OfAccountID: userID,
			FileName:    req.FileName,
			SourceURL:   req.SourceUrl,
			SourceType:  task.ToSourceType(req.SourceType),
			Metadata:    req.Metadata,
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
		t, err := svc.GetTask(ctx, req.ID)
		if err != nil {
			return nil, err
		}

		return &GetTaskResponse{Task: taskToAPI(t)}, nil
	}
}

// Auth endpoints -----------------------------------------------------------
type CreateAccountGatewayRequest struct {
	AccountName string `json:"account_name"`
	Password    string `json:"password"`
}

type CreateAccountGatewayResponse struct {
	ID          uint64 `json:"id"`
	AccountName string `json:"account_name"`
}

type CreateSessionGatewayRequest struct {
	AccountName string `json:"account_name"`
	Password    string `json:"password"`
}

type CreateSessionGatewayResponse struct {
	Token   string       `json:"token"`
	Account *AuthAccount `json:"account,omitempty"`
}

// AuthAccount is a small projection of auth.Account used in gateway responses.
type AuthAccount struct {
	ID          uint64 `json:"id"`
	AccountName string `json:"account_name"`
}

func MakeCreateAccountEndpoint(svc auth.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateAccountGatewayRequest)
		out, err := svc.CreateAccount(ctx, auth.CreateAccountParams{AccountName: req.AccountName, Password: req.Password})
		if err != nil {
			return nil, err
		}
		return &CreateAccountGatewayResponse{ID: out.ID, AccountName: out.AccountName}, nil
	}
}

func MakeCreateSessionEndpoint(svc auth.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateSessionGatewayRequest)
		out, err := svc.CreateSession(ctx, auth.CreateSessionParams{AccountName: req.AccountName, Password: req.Password})
		if err != nil {
			return nil, err
		}
		var acct *AuthAccount
		if out.Account != nil {
			acct = &AuthAccount{ID: out.Account.Id, AccountName: out.Account.AccountName}
		}
		return &CreateSessionGatewayResponse{Token: out.Token, Account: acct}, nil
	}
}

func MakeDeleteTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*DeleteTaskRequest)
		if err := svc.DeleteTask(ctx, req.ID); err != nil {
			return nil, err
		}
		return &DeleteTaskResponse{Success: true}, nil
	}
}

func MakePauseTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*PauseTaskRequest)
		if err := svc.PauseTask(ctx, req.ID); err != nil {
			return nil, err
		}
		return &PauseTaskResponse{Success: true}, nil
	}
}

func MakeResumeTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*ResumeTaskRequest)
		if err := svc.ResumeTask(ctx, req.ID); err != nil {
			return nil, err
		}
		return &ResumeTaskResponse{Success: true}, nil
	}
}

func MakeCancelTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CancelTaskRequest)
		if err := svc.CancelTask(ctx, req.ID); err != nil {
			return nil, err
		}
		return &CancelTaskResponse{Success: true}, nil
	}
}

func MakeRetryTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*RetryTaskRequest)
		if err := svc.RetryTask(ctx, req.ID); err != nil {
			return nil, err
		}
		return &RetryTaskResponse{Success: true}, nil
	}
}

func MakeCheckFileExistsEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CheckFileExistsRequest)
		okExists, err := svc.CheckFileExists(ctx, req.TaskId)
		if err != nil {
			return nil, err
		}
		return &CheckFileExistsResponse{Exists: okExists}, nil
	}
}

func MakeGetTaskProgressEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetTaskProgressRequest)

		progress, err := svc.GetTaskProgress(ctx, req.TaskId)
		if err != nil {
			return nil, err
		}

		var p *float64
		var d, tot *int64
		if progress != nil {
			p = &progress.Progress
			d = &progress.DownloadedBytes
			tot = &progress.TotalBytes
		}

		return &GetTaskProgressResponse{Progress: p, DownloadedBytes: d, TotalBytes: tot}, nil
	}
}

func NewGatewayEndpoints(downloadTaskSvc task.Service, authMW endpoint.Middleware, authSvc auth.Service) GatewayEndpoints {
	var authCreate endpoint.Endpoint
	var authSession endpoint.Endpoint
	if authSvc != nil {
		authCreate = MakeCreateAccountEndpoint(authSvc)
		authSession = MakeCreateSessionEndpoint(authSvc)
	}

	return GatewayEndpoints{
		CreateTaskEndpoint:      authMW(MakeCreateTaskEndpoint(downloadTaskSvc)),
		GetTaskEndpoint:         authMW(RequireTaskOwnerMiddleware(downloadTaskSvc, func(req interface{}) uint64 { return req.(*GetTaskRequest).ID })(MakeGetTaskEndpoint(downloadTaskSvc))),
		ListTasksEndpoint:       authMW(MakeListTasksEndpoint(downloadTaskSvc)),
		DeleteTaskEndpoint:      authMW(RequireTaskOwnerMiddleware(downloadTaskSvc, func(req interface{}) uint64 { return req.(*DeleteTaskRequest).ID })(MakeDeleteTaskEndpoint(downloadTaskSvc))),
		PauseTaskEndpoint:       authMW(RequireTaskOwnerMiddleware(downloadTaskSvc, func(req interface{}) uint64 { return req.(*PauseTaskRequest).ID })(MakePauseTaskEndpoint(downloadTaskSvc))),
		ResumeTaskEndpoint:      authMW(RequireTaskOwnerMiddleware(downloadTaskSvc, func(req interface{}) uint64 { return req.(*ResumeTaskRequest).ID })(MakeResumeTaskEndpoint(downloadTaskSvc))),
		CancelTaskEndpoint:      authMW(RequireTaskOwnerMiddleware(downloadTaskSvc, func(req interface{}) uint64 { return req.(*CancelTaskRequest).ID })(MakeCancelTaskEndpoint(downloadTaskSvc))),
		RetryTaskEndpoint:       authMW(RequireTaskOwnerMiddleware(downloadTaskSvc, func(req interface{}) uint64 { return req.(*RetryTaskRequest).ID })(MakeRetryTaskEndpoint(downloadTaskSvc))),
		CheckFileExistsEndpoint: authMW(RequireTaskOwnerMiddleware(downloadTaskSvc, func(req interface{}) uint64 { return req.(*CheckFileExistsRequest).TaskId })(MakeCheckFileExistsEndpoint(downloadTaskSvc))),
		GetTaskProgressEndpoint: authMW(RequireTaskOwnerMiddleware(downloadTaskSvc, func(req interface{}) uint64 { return req.(*GetTaskProgressRequest).TaskId })(MakeGetTaskProgressEndpoint(downloadTaskSvc))),
		AuthCreateEndpoint:      authCreate,
		AuthSessionEndpoint:     authSession,
	}
}
