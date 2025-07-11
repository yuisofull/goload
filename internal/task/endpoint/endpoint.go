package taskendpoint

import (
	"context"
	"github.com/go-kit/kit/endpoint"
	"github.com/yuisofull/goload/internal/task"
	"github.com/yuisofull/goload/internal/task/pb"
)

type CreateDownloadTaskRequest pb.CreateDownloadTaskRequest

type CreateDownloadTaskResponse pb.CreateDownloadTaskResponse

type GetDownloadTaskRequest pb.GetDownloadTaskRequest

type GetDownloadTaskResponse pb.GetDownloadTaskResponse

type GetDownloadTaskListRequest pb.GetDownloadTaskListRequest

type GetDownloadTaskListResponse pb.GetDownloadTaskListResponse

type UpdateDownloadTaskRequest pb.UpdateDownloadTaskRequest

type UpdateDownloadTaskResponse pb.UpdateDownloadTaskResponse

type DeleteDownloadTaskRequest pb.DeleteDownloadTaskRequest

type DeleteDownloadTaskResponse pb.DeleteDownloadTaskResponse

type Set struct {
	CreateDownloadTaskEndpoint  endpoint.Endpoint
	GetDownloadTaskEndpoint     endpoint.Endpoint
	GetDownloadTaskListEndpoint endpoint.Endpoint
	UpdateDownloadTaskEndpoint  endpoint.Endpoint
	DeleteDownloadTaskEndpoint  endpoint.Endpoint
}

func MakeCreateDownloadTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateDownloadTaskRequest)

		params := task.CreateParams{
			UserID:       req.UserId,
			DownloadType: task.DownloadType(req.DownloadType),
			Url:          req.Url,
		}

		output, err := svc.Create(ctx, params)
		if err != nil {
			return nil, err
		}

		return &CreateDownloadTaskResponse{
			DownloadTask: &pb.DownloadTask{
				Id:             output.DownloadTask.Id,
				OfAccountId:    output.DownloadTask.OfAccountId,
				DownloadType:   pb.DownloadType(output.DownloadTask.DownloadType),
				Url:            output.DownloadTask.Url,
				DownloadStatus: pb.DownloadStatus(output.DownloadTask.DownloadStatus),
			},
		}, nil
	}
}

func MakeGetDownloadTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetDownloadTaskRequest)
		params := task.GetParams{
			UserID:         req.UserId,
			DownloadTaskID: req.DownloadTaskId,
		}
		output, err := svc.Get(ctx, params)
		if err != nil {
			return nil, err
		}
		return &GetDownloadTaskResponse{
			DownloadTask: &pb.DownloadTask{
				Id:             output.DownloadTask.Id,
				OfAccountId:    output.DownloadTask.OfAccountId,
				DownloadType:   pb.DownloadType(output.DownloadTask.DownloadType),
				Url:            output.DownloadTask.Url,
				DownloadStatus: pb.DownloadStatus(output.DownloadTask.DownloadStatus),
			},
		}, nil
	}
}

func MakeGetDownloadTaskListEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetDownloadTaskListRequest)

		params := task.ListParams{
			UserID: req.UserId,
			Offset: req.Offset,
			Limit:  req.Limit,
		}

		output, err := svc.List(ctx, params)
		if err != nil {
			return nil, err
		}
		pbTasks := make([]*pb.DownloadTask, len(output.DownloadTasks))
		for i, downloadTask := range output.DownloadTasks {
			pbTasks[i] = &pb.DownloadTask{
				Id:             downloadTask.Id,
				OfAccountId:    downloadTask.OfAccountId,
				DownloadType:   pb.DownloadType(downloadTask.DownloadType),
				Url:            downloadTask.Url,
				DownloadStatus: pb.DownloadStatus(downloadTask.DownloadStatus),
			}
		}

		return &GetDownloadTaskListResponse{
			DownloadTasks: pbTasks,
			TotalCount:    output.TotalCount,
		}, nil
	}
}

func MakeUpdateDownloadTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*UpdateDownloadTaskRequest)

		params := task.UpdateParams{
			UserID:         req.UserId,
			DownloadTaskId: req.DownloadTaskId,
			Url:            req.Url,
		}

		output, err := svc.Update(ctx, params)
		if err != nil {
			return nil, err
		}
		return &UpdateDownloadTaskResponse{
			DownloadTask: &pb.DownloadTask{
				Id:             output.DownloadTask.Id,
				OfAccountId:    output.DownloadTask.OfAccountId,
				DownloadType:   pb.DownloadType(output.DownloadTask.DownloadType),
				Url:            output.DownloadTask.Url,
				DownloadStatus: pb.DownloadStatus(output.DownloadTask.DownloadStatus),
			},
		}, nil
	}
}

func MakeDeleteDownloadTaskEndpoint(svc task.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*DeleteDownloadTaskRequest)

		params := task.DeleteParams{
			UserID: req.UserId,
			DownloadTask: &task.DownloadTask{
				Id: req.DownloadTaskId,
			},
		}

		err := svc.Delete(ctx, params)
		if err != nil {
			return nil, err
		}
		return &DeleteDownloadTaskResponse{}, nil
	}
}

func New(svc task.Service) Set {
	var createDownloadTaskEndpoint endpoint.Endpoint
	{
		createDownloadTaskEndpoint = MakeCreateDownloadTaskEndpoint(svc)
	}

	var getDownloadTaskListEndpoint endpoint.Endpoint
	{
		getDownloadTaskListEndpoint = MakeGetDownloadTaskListEndpoint(svc)
	}

	var updateDownloadTaskEndpoint endpoint.Endpoint
	{
		updateDownloadTaskEndpoint = MakeUpdateDownloadTaskEndpoint(svc)
	}

	var deleteDownloadTaskEndpoint endpoint.Endpoint
	{
		deleteDownloadTaskEndpoint = MakeDeleteDownloadTaskEndpoint(svc)
	}

	return Set{
		CreateDownloadTaskEndpoint:  createDownloadTaskEndpoint,
		GetDownloadTaskListEndpoint: getDownloadTaskListEndpoint,
		UpdateDownloadTaskEndpoint:  updateDownloadTaskEndpoint,
		DeleteDownloadTaskEndpoint:  deleteDownloadTaskEndpoint,
	}
}

func (s *Set) Create(ctx context.Context, req task.CreateParams) (task.CreateResult, error) {
	resp, err := s.CreateDownloadTaskEndpoint(ctx, &CreateDownloadTaskRequest{
		UserId:       req.UserID,
		DownloadType: pb.DownloadType(req.DownloadType),
		Url:          req.Url,
	})
	if err != nil {
		return task.CreateResult{}, err
	}
	out := resp.(*CreateDownloadTaskResponse)
	return task.CreateResult{
		DownloadTask: &task.DownloadTask{
			Id:             out.DownloadTask.Id,
			OfAccountId:    out.DownloadTask.OfAccountId,
			DownloadType:   task.DownloadType(out.DownloadTask.DownloadType),
			Url:            out.DownloadTask.Url,
			DownloadStatus: task.DownloadStatus(out.DownloadTask.DownloadStatus),
		},
	}, nil
}

func (s *Set) Get(ctx context.Context, req task.GetParams) (task.GetResult, error) {
	resp, err := s.GetDownloadTaskEndpoint(ctx, &GetDownloadTaskRequest{
		UserId:         req.UserID,
		DownloadTaskId: req.DownloadTaskID,
	})
	if err != nil {
		return task.GetResult{}, err
	}
	out := resp.(*GetDownloadTaskResponse)
	return task.GetResult{
		DownloadTask: &task.DownloadTask{
			Id:             out.DownloadTask.Id,
			OfAccountId:    out.DownloadTask.OfAccountId,
			DownloadType:   task.DownloadType(out.DownloadTask.DownloadType),
			Url:            out.DownloadTask.Url,
			DownloadStatus: task.DownloadStatus(out.DownloadTask.DownloadStatus),
		},
	}, nil
}
func (s *Set) List(ctx context.Context, req task.ListParams) (task.ListResult, error) {
	resp, err := s.GetDownloadTaskListEndpoint(ctx, &GetDownloadTaskListRequest{
		UserId: req.UserID,
		Offset: req.Offset,
		Limit:  req.Limit,
	})
	if err != nil {
		return task.ListResult{}, err
	}
	out := resp.(*GetDownloadTaskListResponse)
	pbTasks := out.DownloadTasks
	tasks := make([]*task.DownloadTask, len(pbTasks))
	for i, downloadTask := range pbTasks {
		tasks[i] = &task.DownloadTask{
			Id:             downloadTask.Id,
			OfAccountId:    downloadTask.OfAccountId,
			DownloadType:   task.DownloadType(downloadTask.DownloadType),
			Url:            downloadTask.Url,
			DownloadStatus: task.DownloadStatus(downloadTask.DownloadStatus),
		}
	}
	return task.ListResult{
		DownloadTasks: tasks,
		TotalCount:    out.TotalCount,
	}, nil
}

func (s *Set) Update(ctx context.Context, req task.UpdateParams) (task.UpdateResult, error) {
	resp, err := s.UpdateDownloadTaskEndpoint(ctx, &UpdateDownloadTaskRequest{
		UserId:         req.UserID,
		DownloadTaskId: req.DownloadTaskId,
		Url:            req.Url,
	})
	if err != nil {
		return task.UpdateResult{}, err
	}
	out := resp.(*UpdateDownloadTaskResponse)
	return task.UpdateResult{
		DownloadTask: &task.DownloadTask{
			Id:             out.DownloadTask.Id,
			OfAccountId:    out.DownloadTask.OfAccountId,
			DownloadType:   task.DownloadType(out.DownloadTask.DownloadType),
			Url:            out.DownloadTask.Url,
			DownloadStatus: task.DownloadStatus(out.DownloadTask.DownloadStatus),
		},
	}, nil
}

func (s *Set) Delete(ctx context.Context, req task.DeleteParams) error {
	_, err := s.DeleteDownloadTaskEndpoint(ctx, &DeleteDownloadTaskRequest{
		UserId:         req.UserID,
		DownloadTaskId: req.DownloadTask.Id,
	})
	return err
}
