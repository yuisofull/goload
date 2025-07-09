package downloadtaskendpoint

import (
	"context"
	"github.com/go-kit/kit/endpoint"
	"github.com/yuisofull/goload/internal/downloadtask"
	"github.com/yuisofull/goload/internal/downloadtask/pb"
)

type CreateDownloadTaskRequest pb.CreateDownloadTaskRequest

type CreateDownloadTaskResponse pb.CreateDownloadTaskResponse

type GetDownloadTaskListRequest pb.GetDownloadTaskListRequest

type GetDownloadTaskListResponse pb.GetDownloadTaskListResponse

type UpdateDownloadTaskRequest pb.UpdateDownloadTaskRequest

type UpdateDownloadTaskResponse pb.UpdateDownloadTaskResponse

type DeleteDownloadTaskRequest pb.DeleteDownloadTaskRequest

type DeleteDownloadTaskResponse pb.DeleteDownloadTaskResponse

type Set struct {
	CreateDownloadTaskEndpoint  endpoint.Endpoint
	GetDownloadTaskListEndpoint endpoint.Endpoint
	UpdateDownloadTaskEndpoint  endpoint.Endpoint
	DeleteDownloadTaskEndpoint  endpoint.Endpoint
}

func MakeCreateDownloadTaskEndpoint(svc downloadtask.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateDownloadTaskRequest)

		params := downloadtask.CreateParams{
			UserID:       req.UserId,
			DownloadType: downloadtask.DownloadType(req.DownloadType),
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

func MakeGetDownloadTaskListEndpoint(svc downloadtask.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*GetDownloadTaskListRequest)

		params := downloadtask.ListParams{
			UserID: req.UserId,
			Offset: req.Offset,
			Limit:  req.Limit,
		}

		output, err := svc.List(ctx, params)
		if err != nil {
			return nil, err
		}
		pbTasks := make([]*pb.DownloadTask, len(output.DownloadTasks))
		for i, task := range output.DownloadTasks {
			pbTasks[i] = &pb.DownloadTask{
				Id:             task.Id,
				OfAccountId:    task.OfAccountId,
				DownloadType:   pb.DownloadType(task.DownloadType),
				Url:            task.Url,
				DownloadStatus: pb.DownloadStatus(task.DownloadStatus),
			}
		}

		return &GetDownloadTaskListResponse{
			DownloadTasks: pbTasks,
			TotalCount:    output.TotalCount,
		}, nil
	}
}

func MakeUpdateDownloadTaskEndpoint(svc downloadtask.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*UpdateDownloadTaskRequest)

		params := downloadtask.UpdateParams{
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

func MakeDeleteDownloadTaskEndpoint(svc downloadtask.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*DeleteDownloadTaskRequest)

		params := downloadtask.DeleteParams{
			UserID: req.UserId,
			DownloadTask: &downloadtask.DownloadTask{
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

func New(svc downloadtask.Service) Set {
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

func (s *Set) Create(ctx context.Context, req downloadtask.CreateParams) (downloadtask.CreateResult, error) {
	resp, err := s.CreateDownloadTaskEndpoint(ctx, &CreateDownloadTaskRequest{
		UserId:       req.UserID,
		DownloadType: pb.DownloadType(req.DownloadType),
		Url:          req.Url,
	})
	if err != nil {
		return downloadtask.CreateResult{}, err
	}
	out := resp.(*CreateDownloadTaskResponse)
	return downloadtask.CreateResult{
		DownloadTask: &downloadtask.DownloadTask{
			Id:             out.DownloadTask.Id,
			OfAccountId:    out.DownloadTask.OfAccountId,
			DownloadType:   downloadtask.DownloadType(out.DownloadTask.DownloadType),
			Url:            out.DownloadTask.Url,
			DownloadStatus: downloadtask.DownloadStatus(out.DownloadTask.DownloadStatus),
		},
	}, nil
}

func (s *Set) List(ctx context.Context, req downloadtask.ListParams) (downloadtask.ListResult, error) {
	resp, err := s.GetDownloadTaskListEndpoint(ctx, &GetDownloadTaskListRequest{
		UserId: req.UserID,
		Offset: req.Offset,
		Limit:  req.Limit,
	})
	if err != nil {
		return downloadtask.ListResult{}, err
	}
	out := resp.(*GetDownloadTaskListResponse)
	pbTasks := out.DownloadTasks
	tasks := make([]*downloadtask.DownloadTask, len(pbTasks))
	for i, task := range pbTasks {
		tasks[i] = &downloadtask.DownloadTask{
			Id:             task.Id,
			OfAccountId:    task.OfAccountId,
			DownloadType:   downloadtask.DownloadType(task.DownloadType),
			Url:            task.Url,
			DownloadStatus: downloadtask.DownloadStatus(task.DownloadStatus),
		}
	}
	return downloadtask.ListResult{
		DownloadTasks: tasks,
		TotalCount:    out.TotalCount,
	}, nil
}

func (s *Set) Update(ctx context.Context, req downloadtask.UpdateParams) (downloadtask.UpdateResult, error) {
	resp, err := s.UpdateDownloadTaskEndpoint(ctx, &UpdateDownloadTaskRequest{
		UserId:         req.UserID,
		DownloadTaskId: req.DownloadTaskId,
		Url:            req.Url,
	})
	if err != nil {
		return downloadtask.UpdateResult{}, err
	}
	out := resp.(*UpdateDownloadTaskResponse)
	return downloadtask.UpdateResult{
		DownloadTask: &downloadtask.DownloadTask{
			Id:             out.DownloadTask.Id,
			OfAccountId:    out.DownloadTask.OfAccountId,
			DownloadType:   downloadtask.DownloadType(out.DownloadTask.DownloadType),
			Url:            out.DownloadTask.Url,
			DownloadStatus: downloadtask.DownloadStatus(out.DownloadTask.DownloadStatus),
		},
	}, nil
}

func (s *Set) Delete(ctx context.Context, req downloadtask.DeleteParams) error {
	_, err := s.DeleteDownloadTaskEndpoint(ctx, &DeleteDownloadTaskRequest{
		UserId:         req.UserID,
		DownloadTaskId: req.DownloadTask.Id,
	})
	return err
}
