package apigateway

import (
	"context"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/samber/lo"
	"github.com/yuisofull/goload/internal/task"
)

type ListTasksRequest struct {
	Filter *struct {
		OfAccountID uint64
	}
	Offset  int32
	Limit   int32
	SortBy  string
	SortAsc bool
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

type GatewayEndpoints struct {
	ListTasksEndpoint endpoint.Endpoint
}

func MakeListTasksEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*ListTasksRequest)

		params := task.ListTasksParam{
			Filter: &task.TaskFilter{
				OfAccountID: req.Filter.OfAccountID,
			},
			Offset:  req.Offset,
			Limit:   req.Limit,
			SortBy:  req.SortBy,
			SortAsc: req.SortAsc,
		}

		result, err := svc.ListTasks(ctx, &params)
		if err != nil {
			return nil, err
		}

		return &ListTasksResponse{
			Tasks: lo.Map(result.Tasks, func(task *task.Task, _ int) *Task {
				return &Task{
					ID:              task.ID,
					OfAccountID:     task.OfAccountID,
					FileName:        task.FileName,
					SourceUrl:       task.SourceURL,
					SourceType:      task.SourceType.String(),
					ChecksumType:    &task.Checksum.ChecksumType,
					ChecksumValue:   &task.Checksum.ChecksumValue,
					Status:          task.Status.String(),
					Progress:        &task.Progress.Progress,
					DownloadedBytes: &task.Progress.DownloadedBytes,
					TotalBytes:      &task.Progress.TotalBytes,
					ErrorMessage:    task.ErrorMessage,
					Metadata:        task.Metadata,
					CreatedAt:       &task.CreatedAt,
					UpdatedAt:       &task.UpdatedAt,
					CompletedAt:     task.CompletedAt,
				}
			}),
			TotalCount: result.Total,
		}, nil
	}
}

func NewGatewayEndpoints(downloadTaskSvc task.Service, authMW endpoint.Middleware) GatewayEndpoints {
	return GatewayEndpoints{
		ListTasksEndpoint: authMW(MakeListTasksEndpoint(downloadTaskSvc)),
	}
}
