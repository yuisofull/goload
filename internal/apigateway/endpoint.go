package apigateway

import (
	"context"
	"github.com/go-kit/kit/endpoint"
	"github.com/yuisofull/goload/internal/file"
	"github.com/yuisofull/goload/internal/task_v2"
)

type CreateDownloadTaskRequest struct {
	UserID       uint64
	DownloadType file.DownloadType
	URL          string
}

type CreateDownloadTaskResponse struct {
	DownloadTask *HTTPDownloadTask `json:"download_task"`
}

type GetDownloadTaskListRequest struct {
	UserID uint64
	Offset uint64
	Limit  uint64
}

type GetDownloadTaskListResponse struct {
	DownloadTasks []*HTTPDownloadTask `json:"download_tasks"`
	TotalCount    uint64              `json:"total_count"`
}

type UpdateDownloadTaskRequest struct {
	UserID         uint64
	DownloadTaskID uint64
	URL            string
}

type UpdateDownloadTaskResponse struct {
	DownloadTask *HTTPDownloadTask `json:"download_task"`
}

type DeleteDownloadTaskRequest struct {
	UserID         uint64
	DownloadTaskID uint64
}

type DeleteDownloadTaskResponse struct{}

// GatewayEndpoints holds all endpoints from all services
type GatewayEndpoints struct {
	// Download Task Service endpoints
	CreateDownloadTask  endpoint.Endpoint
	GetDownloadTaskList endpoint.Endpoint
	UpdateDownloadTask  endpoint.Endpoint
	DeleteDownloadTask  endpoint.Endpoint

	// Auth Service endpoints (for future use)
	// Login    endpoint.Endpoint
	// Register endpoint.Endpoint

	// File Service endpoints (for future use)
	// GetFile endpoint.Endpoint
}

// DownloadTaskEndpoints holds all download task endpoints (for backward compatibility)
type DownloadTaskEndpoints struct {
	CreateDownloadTask  endpoint.Endpoint
	GetDownloadTaskList endpoint.Endpoint
	UpdateDownloadTask  endpoint.Endpoint
	DeleteDownloadTask  endpoint.Endpoint
}

// MakeCreateDownloadTaskEndpoint creates endpoint for creating download tasks
func MakeCreateDownloadTaskEndpoint(svc task_v2.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateDownloadTaskRequest)

		params := task_v2.CreateParams{
			UserID:       req.UserID,
			DownloadType: req.DownloadType,
			Url:          req.URL,
		}

		result, err := svc.Create(ctx, params)
		if err != nil {
			return nil, err
		}

		return &CreateDownloadTaskResponse{
			DownloadTask: &HTTPDownloadTask{
				ID:             result.DownloadTask.Id,
				OfAccountID:    result.DownloadTask.OfAccountId,
				DownloadType:   int(result.DownloadTask.DownloadType),
				URL:            result.DownloadTask.Url,
				DownloadStatus: int(result.DownloadTask.DownloadStatus),
			},
		}, nil
	}
}

// MakeGetDownloadTaskListEndpoint creates endpoint for listing download tasks
func MakeGetDownloadTaskListEndpoint(svc task_v2.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetDownloadTaskListRequest)

		params := task_v2.ListParams{
			UserID: req.UserID,
			Offset: req.Offset,
			Limit:  req.Limit,
		}

		result, err := svc.List(ctx, params)
		if err != nil {
			return nil, err
		}

		httpTasks := make([]*HTTPDownloadTask, len(result.DownloadTasks))
		for i, downloadTask := range result.DownloadTasks {
			httpTasks[i] = &HTTPDownloadTask{
				ID:             downloadTask.Id,
				OfAccountID:    downloadTask.OfAccountId,
				DownloadType:   int(downloadTask.DownloadType),
				URL:            downloadTask.Url,
				DownloadStatus: int(downloadTask.DownloadStatus),
			}
		}

		return &GetDownloadTaskListResponse{
			DownloadTasks: httpTasks,
			TotalCount:    result.TotalCount,
		}, nil
	}
}

// MakeUpdateDownloadTaskEndpoint creates endpoint for updating download tasks
func MakeUpdateDownloadTaskEndpoint(svc task_v2.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*UpdateDownloadTaskRequest)

		params := task_v2.UpdateParams{
			UserID:         req.UserID,
			DownloadTaskId: req.DownloadTaskID,
			Url:            req.URL,
		}

		result, err := svc.Update(ctx, params)
		if err != nil {
			return nil, err
		}

		return &UpdateDownloadTaskResponse{
			DownloadTask: &HTTPDownloadTask{
				ID:             result.DownloadTask.Id,
				OfAccountID:    result.DownloadTask.OfAccountId,
				DownloadType:   int(result.DownloadTask.DownloadType),
				URL:            result.DownloadTask.Url,
				DownloadStatus: int(result.DownloadTask.DownloadStatus),
			},
		}, nil
	}
}

// MakeDeleteDownloadTaskEndpoint creates endpoint for deleting download tasks
func MakeDeleteDownloadTaskEndpoint(svc task_v2.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*DeleteDownloadTaskRequest)

		params := task_v2.DeleteParams{
			UserID: req.UserID,
			DownloadTask: &task_v2.DownloadTask{
				Id: req.DownloadTaskID,
			},
		}

		err := svc.Delete(ctx, params)
		if err != nil {
			return nil, err
		}

		return &DeleteDownloadTaskResponse{}, nil
	}
}

// NewGatewayEndpoints creates a new set of all gateway endpoints
func NewGatewayEndpoints(downloadTaskSvc task_v2.Service, authMW endpoint.Middleware) GatewayEndpoints {
	return GatewayEndpoints{
		CreateDownloadTask:  authMW(MakeCreateDownloadTaskEndpoint(downloadTaskSvc)),
		GetDownloadTaskList: authMW(MakeGetDownloadTaskListEndpoint(downloadTaskSvc)),
		UpdateDownloadTask:  authMW(MakeUpdateDownloadTaskEndpoint(downloadTaskSvc)),
		DeleteDownloadTask:  authMW(MakeDeleteDownloadTaskEndpoint(downloadTaskSvc)),
	}
}
