package taskendpoint_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperrors "github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/storage"
	"github.com/yuisofull/goload/internal/task"
	taskendpoint "github.com/yuisofull/goload/internal/task/endpoint"
	pb "github.com/yuisofull/goload/internal/task/pb"
)

// ---------------------------------------------------------------------------
// Mock service
// ---------------------------------------------------------------------------

type mockTaskService struct {
	createTaskFn          func(ctx context.Context, param *task.CreateTaskParam) (*task.Task, error)
	getTaskFn             func(ctx context.Context, id uint64) (*task.Task, error)
	listTasksFn           func(ctx context.Context, param *task.ListTasksParam) (*task.ListTasksOutput, error)
	deleteTaskFn          func(ctx context.Context, id uint64) error
	pauseTaskFn           func(ctx context.Context, id uint64) error
	resumeTaskFn          func(ctx context.Context, id uint64) error
	cancelTaskFn          func(ctx context.Context, id uint64) error
	retryTaskFn           func(ctx context.Context, id uint64) error
	updateStoragePathFn   func(ctx context.Context, id uint64, path string) error
	updateStatusFn        func(ctx context.Context, id uint64, status task.TaskStatus) error
	updateProgressFn      func(ctx context.Context, id uint64, progress task.DownloadProgress) error
	updateErrorFn         func(ctx context.Context, id uint64, err error) error
	completeTaskFn        func(ctx context.Context, id uint64) error
	checkFileExistsFn     func(ctx context.Context, id uint64) (bool, error)
	getTaskProgressFn     func(ctx context.Context, id uint64) (*task.DownloadProgress, error)
	updateChecksumFn      func(ctx context.Context, id uint64, checksum *task.ChecksumInfo) error
	updateMetadataFn      func(ctx context.Context, id uint64, meta map[string]any) error
	updateFileNameFn      func(ctx context.Context, id uint64, fileName string) error
	updateStorageInfoFn   func(ctx context.Context, id uint64, stype storage.Type, path string) error
	generateDownloadURLFn func(ctx context.Context, taskID uint64, ttl time.Duration, oneTime bool) (string, bool, error)
}

func (m *mockTaskService) CreateTask(ctx context.Context, param *task.CreateTaskParam) (*task.Task, error) {
	return m.createTaskFn(ctx, param)
}
func (m *mockTaskService) GetTask(ctx context.Context, id uint64) (*task.Task, error) {
	return m.getTaskFn(ctx, id)
}
func (m *mockTaskService) ListTasks(ctx context.Context, param *task.ListTasksParam) (*task.ListTasksOutput, error) {
	return m.listTasksFn(ctx, param)
}
func (m *mockTaskService) DeleteTask(ctx context.Context, id uint64) error {
	return m.deleteTaskFn(ctx, id)
}
func (m *mockTaskService) PauseTask(ctx context.Context, id uint64) error {
	return m.pauseTaskFn(ctx, id)
}
func (m *mockTaskService) ResumeTask(ctx context.Context, id uint64) error {
	return m.resumeTaskFn(ctx, id)
}
func (m *mockTaskService) CancelTask(ctx context.Context, id uint64) error {
	return m.cancelTaskFn(ctx, id)
}
func (m *mockTaskService) RetryTask(ctx context.Context, id uint64) error {
	return m.retryTaskFn(ctx, id)
}
func (m *mockTaskService) UpdateTaskStoragePath(ctx context.Context, id uint64, path string) error {
	return m.updateStoragePathFn(ctx, id, path)
}
func (m *mockTaskService) UpdateTaskStatus(ctx context.Context, id uint64, status task.TaskStatus) error {
	return m.updateStatusFn(ctx, id, status)
}
func (m *mockTaskService) UpdateTaskProgress(ctx context.Context, id uint64, progress task.DownloadProgress) error {
	return m.updateProgressFn(ctx, id, progress)
}
func (m *mockTaskService) UpdateTaskError(ctx context.Context, id uint64, err error) error {
	return m.updateErrorFn(ctx, id, err)
}
func (m *mockTaskService) CompleteTask(ctx context.Context, id uint64) error {
	return m.completeTaskFn(ctx, id)
}
func (m *mockTaskService) CheckFileExists(ctx context.Context, id uint64) (bool, error) {
	return m.checkFileExistsFn(ctx, id)
}
func (m *mockTaskService) GetTaskProgress(ctx context.Context, id uint64) (*task.DownloadProgress, error) {
	return m.getTaskProgressFn(ctx, id)
}
func (m *mockTaskService) UpdateTaskChecksum(ctx context.Context, id uint64, checksum *task.ChecksumInfo) error {
	return m.updateChecksumFn(ctx, id, checksum)
}
func (m *mockTaskService) UpdateTaskMetadata(ctx context.Context, id uint64, meta map[string]any) error {
	return m.updateMetadataFn(ctx, id, meta)
}
func (m *mockTaskService) UpdateFileName(ctx context.Context, id uint64, fileName string) error {
	if m.updateFileNameFn != nil {
		return m.updateFileNameFn(ctx, id, fileName)
	}
	return nil
}
func (m *mockTaskService) UpdateStorageInfo(ctx context.Context, id uint64, stype storage.Type, path string) error {
	if m.updateStorageInfoFn != nil {
		return m.updateStorageInfoFn(ctx, id, stype, path)
	}
	return nil
}

func (m *mockTaskService) GenerateDownloadURL(
	ctx context.Context,
	taskID uint64,
	ttl time.Duration,
	oneTime bool,
) (string, bool, error) {
	if m.generateDownloadURLFn != nil {
		return m.generateDownloadURLFn(ctx, taskID, ttl, oneTime)
	}
	return "", false, errors.New("not implemented")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func stubTask(id uint64) *task.Task {
	return &task.Task{
		ID:          id,
		OfAccountID: 1,
		FileName:    "file.zip",
		SourceURL:   "https://example.com/file.zip",
		SourceType:  task.SourceHTTPS,
		Status:      task.StatusPending,
		Checksum:    &task.ChecksumInfo{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// svcWithGet returns a minimal mock where GetTask returns stubTask(id) —
// used by endpoints that fetch the task after mutation.
func svcWithGet(base *mockTaskService) *mockTaskService {
	base.getTaskFn = func(_ context.Context, id uint64) (*task.Task, error) {
		return stubTask(id), nil
	}
	return base
}

// ---------------------------------------------------------------------------
// CreateTask endpoint
// ---------------------------------------------------------------------------

func TestMakeCreateTaskEndpoint_Success(t *testing.T) {
	created := stubTask(10)
	svc := svcWithGet(&mockTaskService{
		createTaskFn: func(_ context.Context, param *task.CreateTaskParam) (*task.Task, error) {
			assert.Equal(t, uint64(1), param.OfAccountID)
			assert.Equal(t, "https://example.com/file.zip", param.SourceURL)
			return created, nil
		},
	})

	ep := taskendpoint.MakeCreateTaskEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.CreateTaskRequest{
		OfAccountId: 1,
		FileName:    "file.zip",
		SourceUrl:   "https://example.com/file.zip",
	})

	require.NoError(t, err)
	out := resp.(*taskendpoint.TaskResponse)
	require.NotNil(t, out.Task)
	assert.Equal(t, uint64(10), out.Task.Id)
}

func TestMakeCreateTaskEndpoint_MissingSourceURL(t *testing.T) {
	svc := &mockTaskService{
		createTaskFn: func(_ context.Context, _ *task.CreateTaskParam) (*task.Task, error) {
			return nil, &apperrors.Error{Code: apperrors.ErrCodeInvalidInput, Message: "SourceURL is required"}
		},
	}

	ep := taskendpoint.MakeCreateTaskEndpoint(svc)
	_, err := ep(context.Background(), &taskendpoint.CreateTaskRequest{SourceUrl: ""})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
}

// ---------------------------------------------------------------------------
// GetTask endpoint
// ---------------------------------------------------------------------------

func TestMakeGetTaskEndpoint_Success(t *testing.T) {
	svc := &mockTaskService{
		getTaskFn: func(_ context.Context, id uint64) (*task.Task, error) {
			assert.Equal(t, uint64(5), id)
			return stubTask(5), nil
		},
	}

	ep := taskendpoint.MakeGetTaskEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.GetTaskRequest{Id: 5})

	require.NoError(t, err)
	out := resp.(*taskendpoint.TaskResponse)
	assert.Equal(t, uint64(5), out.Task.Id)
}

func TestMakeGetTaskEndpoint_NotFound(t *testing.T) {
	svc := &mockTaskService{
		getTaskFn: func(_ context.Context, _ uint64) (*task.Task, error) {
			return nil, &apperrors.Error{Code: apperrors.ErrCodeNotFound, Message: "Task not found"}
		},
	}

	ep := taskendpoint.MakeGetTaskEndpoint(svc)
	_, err := ep(context.Background(), &taskendpoint.GetTaskRequest{Id: 999})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeNotFound))
}

// ---------------------------------------------------------------------------
// ListTasks endpoint
// ---------------------------------------------------------------------------

func TestMakeListTasksEndpoint_Success(t *testing.T) {
	svc := &mockTaskService{
		listTasksFn: func(_ context.Context, param *task.ListTasksParam) (*task.ListTasksOutput, error) {
			assert.Equal(t, uint64(1), param.Filter.OfAccountID)
			assert.Equal(t, int32(10), param.Limit)
			assert.Equal(t, int32(0), param.Offset)
			return &task.ListTasksOutput{
				Tasks: []*task.Task{stubTask(1), stubTask(2)},
				Total: 2,
			}, nil
		},
	}

	ep := taskendpoint.MakeListTasksEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.ListTasksRequest{
		Filter: &pb.TaskFilter{OfAccountId: 1},
		Limit:  10,
		Offset: 0,
	})

	require.NoError(t, err)
	out := resp.(*taskendpoint.ListTasksResponse)
	assert.Equal(t, int32(2), out.Total)
	assert.Len(t, out.Tasks, 2)
}

func TestMakeListTasksEndpoint_NilFilter(t *testing.T) {
	svc := &mockTaskService{
		listTasksFn: func(_ context.Context, param *task.ListTasksParam) (*task.ListTasksOutput, error) {
			assert.Equal(t, uint64(0), param.Filter.OfAccountID)
			return &task.ListTasksOutput{Tasks: nil, Total: 0}, nil
		},
	}

	ep := taskendpoint.MakeListTasksEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.ListTasksRequest{Filter: nil})

	require.NoError(t, err)
	out := resp.(*taskendpoint.ListTasksResponse)
	assert.Equal(t, int32(0), out.Total)
}

// ---------------------------------------------------------------------------
// DeleteTask endpoint
// ---------------------------------------------------------------------------

func TestMakeDeleteTaskEndpoint_Success(t *testing.T) {
	svc := &mockTaskService{
		deleteTaskFn: func(_ context.Context, id uint64) error {
			assert.Equal(t, uint64(3), id)
			return nil
		},
	}

	ep := taskendpoint.MakeDeleteTaskEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.DeleteTaskRequest{Id: 3})

	require.NoError(t, err)
	out := resp.(*taskendpoint.DeleteTaskResponse)
	assert.Equal(t, "deleted", out.Message)
}

func TestMakeDeleteTaskEndpoint_NotFound(t *testing.T) {
	svc := &mockTaskService{
		deleteTaskFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{Code: apperrors.ErrCodeNotFound, Message: "Task not found"}
		},
	}

	ep := taskendpoint.MakeDeleteTaskEndpoint(svc)
	_, err := ep(context.Background(), &taskendpoint.DeleteTaskRequest{Id: 99})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeNotFound))
}

// ---------------------------------------------------------------------------
// PauseTask endpoint
// ---------------------------------------------------------------------------

func TestMakePauseTaskEndpoint_Success(t *testing.T) {
	svc := &mockTaskService{
		pauseTaskFn: func(_ context.Context, id uint64) error {
			assert.Equal(t, uint64(7), id)
			return nil
		},
	}

	ep := taskendpoint.MakePauseTaskEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.PauseTaskRequest{Id: 7})

	require.NoError(t, err)
	out := resp.(*taskendpoint.PauseTaskResponse)
	assert.Equal(t, "paused", out.Message)
}

func TestMakePauseTaskEndpoint_InvalidState(t *testing.T) {
	svc := &mockTaskService{
		pauseTaskFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{
				Code:    apperrors.ErrCodeInvalidInput,
				Message: "Only downloading tasks can be paused",
			}
		},
	}

	ep := taskendpoint.MakePauseTaskEndpoint(svc)
	_, err := ep(context.Background(), &taskendpoint.PauseTaskRequest{Id: 7})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
}

// ---------------------------------------------------------------------------
// ResumeTask endpoint
// ---------------------------------------------------------------------------

func TestMakeResumeTaskEndpoint_Success(t *testing.T) {
	svc := &mockTaskService{
		resumeTaskFn: func(_ context.Context, id uint64) error {
			assert.Equal(t, uint64(8), id)
			return nil
		},
	}

	ep := taskendpoint.MakeResumeTaskEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.ResumeTaskRequest{Id: 8})

	require.NoError(t, err)
	out := resp.(*taskendpoint.ResumeTaskResponse)
	assert.Equal(t, "resumed", out.Message)
}

func TestMakeResumeTaskEndpoint_InvalidState(t *testing.T) {
	svc := &mockTaskService{
		resumeTaskFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{Code: apperrors.ErrCodeInvalidInput, Message: "Only paused tasks can be resumed"}
		},
	}

	ep := taskendpoint.MakeResumeTaskEndpoint(svc)
	_, err := ep(context.Background(), &taskendpoint.ResumeTaskRequest{Id: 8})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
}

// ---------------------------------------------------------------------------
// CancelTask endpoint
// ---------------------------------------------------------------------------

func TestMakeCancelTaskEndpoint_Success(t *testing.T) {
	svc := &mockTaskService{
		cancelTaskFn: func(_ context.Context, id uint64) error {
			assert.Equal(t, uint64(9), id)
			return nil
		},
	}

	ep := taskendpoint.MakeCancelTaskEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.CancelTaskRequest{Id: 9})

	require.NoError(t, err)
	out := resp.(*taskendpoint.CancelTaskResponse)
	assert.Equal(t, "cancelled", out.Message)
}

func TestMakeCancelTaskEndpoint_AlreadyCompleted(t *testing.T) {
	svc := &mockTaskService{
		cancelTaskFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{
				Code:    apperrors.ErrCodeInvalidInput,
				Message: "Completed or cancelled tasks cannot be cancelled",
			}
		},
	}

	ep := taskendpoint.MakeCancelTaskEndpoint(svc)
	_, err := ep(context.Background(), &taskendpoint.CancelTaskRequest{Id: 9})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
}

// ---------------------------------------------------------------------------
// RetryTask endpoint
// ---------------------------------------------------------------------------

func TestMakeRetryTaskEndpoint_Success(t *testing.T) {
	svc := &mockTaskService{
		retryTaskFn: func(_ context.Context, id uint64) error {
			assert.Equal(t, uint64(11), id)
			return nil
		},
	}

	ep := taskendpoint.MakeRetryTaskEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.RetryTaskRequest{Id: 11})

	require.NoError(t, err)
	out := resp.(*taskendpoint.RetryTaskResponse)
	assert.Equal(t, "retried", out.Message)
}

func TestMakeRetryTaskEndpoint_NotFailed(t *testing.T) {
	svc := &mockTaskService{
		retryTaskFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{Code: apperrors.ErrCodeInvalidInput, Message: "Only failed tasks can be retried"}
		},
	}

	ep := taskendpoint.MakeRetryTaskEndpoint(svc)
	_, err := ep(context.Background(), &taskendpoint.RetryTaskRequest{Id: 11})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
}

// ---------------------------------------------------------------------------
// UpdateTaskStoragePath endpoint
// ---------------------------------------------------------------------------

func TestMakeUpdateTaskStoragePathEndpoint_Success(t *testing.T) {
	svc := svcWithGet(&mockTaskService{
		updateStoragePathFn: func(_ context.Context, id uint64, path string) error {
			assert.Equal(t, uint64(12), id)
			assert.Equal(t, "/data/file.zip", path)
			return nil
		},
	})

	ep := taskendpoint.MakeUpdateTaskStoragePathEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.UpdateTaskStoragePathRequest{
		Id:          12,
		StoragePath: "/data/file.zip",
	})

	require.NoError(t, err)
	out := resp.(*taskendpoint.TaskResponse)
	assert.NotNil(t, out.Task)
}

// ---------------------------------------------------------------------------
// UpdateTaskStatus endpoint
// ---------------------------------------------------------------------------

func TestMakeUpdateTaskStatusEndpoint_Success(t *testing.T) {
	svc := svcWithGet(&mockTaskService{
		updateStatusFn: func(_ context.Context, id uint64, status task.TaskStatus) error {
			assert.Equal(t, uint64(13), id)
			return nil
		},
	})

	ep := taskendpoint.MakeUpdateTaskStatusEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.UpdateTaskStatusRequest{Id: 13})

	require.NoError(t, err)
	assert.NotNil(t, resp.(*taskendpoint.TaskResponse).Task)
}

// ---------------------------------------------------------------------------
// UpdateTaskProgress endpoint
// ---------------------------------------------------------------------------

func TestMakeUpdateTaskProgressEndpoint_WithProgress(t *testing.T) {
	progressCalled := false
	svc := svcWithGet(&mockTaskService{
		updateProgressFn: func(_ context.Context, id uint64, p task.DownloadProgress) error {
			assert.Equal(t, uint64(14), id)
			assert.Equal(t, int64(1024), p.TotalBytes)
			progressCalled = true
			return nil
		},
	})

	ep := taskendpoint.MakeUpdateTaskProgressEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.UpdateTaskProgressRequest{
		Id: 14,
		Progress: &pb.DownloadProgress{
			TotalBytes: 1024,
		},
	})

	require.NoError(t, err)
	assert.True(t, progressCalled)
	assert.NotNil(t, resp.(*taskendpoint.TaskResponse).Task)
}

func TestMakeUpdateTaskProgressEndpoint_NilProgress(t *testing.T) {
	svc := svcWithGet(&mockTaskService{
		updateProgressFn: func(_ context.Context, _ uint64, _ task.DownloadProgress) error {
			t.Fatal("updateProgress should not be called when progress is nil")
			return nil
		},
	})

	ep := taskendpoint.MakeUpdateTaskProgressEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.UpdateTaskProgressRequest{Id: 14, Progress: nil})

	require.NoError(t, err)
	assert.NotNil(t, resp.(*taskendpoint.TaskResponse).Task)
}

// ---------------------------------------------------------------------------
// UpdateTaskError endpoint
// ---------------------------------------------------------------------------

func TestMakeUpdateTaskErrorEndpoint_Success(t *testing.T) {
	svc := svcWithGet(&mockTaskService{
		updateErrorFn: func(_ context.Context, id uint64, err error) error {
			assert.Equal(t, uint64(15), id)
			assert.EqualError(t, err, "download failed")
			return nil
		},
	})

	ep := taskendpoint.MakeUpdateTaskErrorEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.UpdateTaskErrorRequest{
		Id:    15,
		Error: "download failed",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp.(*taskendpoint.TaskResponse).Task)
}

// ---------------------------------------------------------------------------
// CompleteTask endpoint
// ---------------------------------------------------------------------------

func TestMakeCompleteTaskEndpoint_Success(t *testing.T) {
	svc := svcWithGet(&mockTaskService{
		completeTaskFn: func(_ context.Context, id uint64) error {
			assert.Equal(t, uint64(16), id)
			return nil
		},
	})

	ep := taskendpoint.MakeCompleteTaskEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.CompleteTaskRequest{Id: 16})

	require.NoError(t, err)
	assert.NotNil(t, resp.(*taskendpoint.TaskResponse).Task)
}

func TestMakeCompleteTaskEndpoint_InternalError(t *testing.T) {
	svc := &mockTaskService{
		completeTaskFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{Code: apperrors.ErrCodeInternal, Message: "complete task failed"}
		},
	}

	ep := taskendpoint.MakeCompleteTaskEndpoint(svc)
	_, err := ep(context.Background(), &taskendpoint.CompleteTaskRequest{Id: 16})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInternal))
}

// ---------------------------------------------------------------------------
// CheckFileExists endpoint
// ---------------------------------------------------------------------------

func TestMakeCheckFileExistsEndpoint_Exists(t *testing.T) {
	svc := &mockTaskService{
		checkFileExistsFn: func(_ context.Context, id uint64) (bool, error) {
			assert.Equal(t, uint64(20), id)
			return true, nil
		},
	}

	ep := taskendpoint.MakeCheckFileExistsEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.CheckFileExistsRequest{TaskId: 20})

	require.NoError(t, err)
	out := resp.(*taskendpoint.CheckFileExistsResponse)
	assert.True(t, out.Exists)
}

func TestMakeCheckFileExistsEndpoint_NotExists(t *testing.T) {
	svc := &mockTaskService{
		checkFileExistsFn: func(_ context.Context, _ uint64) (bool, error) { return false, nil },
	}

	ep := taskendpoint.MakeCheckFileExistsEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.CheckFileExistsRequest{TaskId: 21})

	require.NoError(t, err)
	out := resp.(*taskendpoint.CheckFileExistsResponse)
	assert.False(t, out.Exists)
}

func TestMakeCheckFileExistsEndpoint_NotFound(t *testing.T) {
	svc := &mockTaskService{
		checkFileExistsFn: func(_ context.Context, _ uint64) (bool, error) {
			return false, &apperrors.Error{Code: apperrors.ErrCodeNotFound, Message: "task not found"}
		},
	}

	ep := taskendpoint.MakeCheckFileExistsEndpoint(svc)
	_, err := ep(context.Background(), &taskendpoint.CheckFileExistsRequest{TaskId: 99})

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeNotFound))
}

// ---------------------------------------------------------------------------
// GetTaskProgress endpoint
// ---------------------------------------------------------------------------

func TestMakeGetTaskProgressEndpoint_WithProgress(t *testing.T) {
	svc := &mockTaskService{
		getTaskProgressFn: func(_ context.Context, id uint64) (*task.DownloadProgress, error) {
			assert.Equal(t, uint64(22), id)
			return &task.DownloadProgress{TotalBytes: 2048, DownloadedBytes: 512}, nil
		},
	}

	ep := taskendpoint.MakeGetTaskProgressEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.GetTaskProgressRequest{TaskId: 22})

	require.NoError(t, err)
	out := resp.(*taskendpoint.GetTaskProgressResponse)
	require.NotNil(t, out.Progress)
	assert.Equal(t, int64(2048), out.Progress.TotalBytes)
}

func TestMakeGetTaskProgressEndpoint_NilProgress(t *testing.T) {
	svc := &mockTaskService{
		getTaskProgressFn: func(_ context.Context, _ uint64) (*task.DownloadProgress, error) {
			return nil, nil
		},
	}

	ep := taskendpoint.MakeGetTaskProgressEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.GetTaskProgressRequest{TaskId: 23})

	require.NoError(t, err)
	out := resp.(*taskendpoint.GetTaskProgressResponse)
	assert.Nil(t, out.Progress)
}

// ---------------------------------------------------------------------------
// UpdateTaskChecksum endpoint
// ---------------------------------------------------------------------------

func TestMakeUpdateTaskChecksumEndpoint_Success(t *testing.T) {
	svc := &mockTaskService{
		updateChecksumFn: func(_ context.Context, id uint64, checksum *task.ChecksumInfo) error {
			assert.Equal(t, uint64(25), id)
			assert.Equal(t, "sha256", checksum.ChecksumType)
			assert.Equal(t, "abc123", checksum.ChecksumValue)
			return nil
		},
	}

	ep := taskendpoint.MakeUpdateTaskChecksumEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.UpdateTaskChecksumRequest{
		TaskId: 25,
		Checksum: &pb.ChecksumInfo{
			ChecksumType:  "sha256",
			ChecksumValue: "abc123",
		},
	})

	require.NoError(t, err)
	assert.Nil(t, resp)
}

// ---------------------------------------------------------------------------
// UpdateTaskMetadata endpoint
// ---------------------------------------------------------------------------

func TestMakeUpdateTaskMetadataEndpoint_Success(t *testing.T) {
	svc := &mockTaskService{
		updateMetadataFn: func(_ context.Context, id uint64, meta map[string]any) error {
			assert.Equal(t, uint64(26), id)
			assert.Equal(t, "bar", meta["foo"])
			return nil
		},
	}

	ep := taskendpoint.MakeUpdateTaskMetadataEndpoint(svc)
	resp, err := ep(context.Background(), &taskendpoint.UpdateTaskMetadataRequest{
		TaskId:   26,
		Metadata: mustNewStruct(t, map[string]any{"foo": "bar"}),
	})

	require.NoError(t, err)
	assert.Nil(t, resp)
}

// ---------------------------------------------------------------------------
// Full Set — business logic round-trips
// ---------------------------------------------------------------------------

func TestSet_CreateTask_RoundTrip(t *testing.T) {
	created := stubTask(100)
	svc := svcWithGet(&mockTaskService{
		createTaskFn: func(_ context.Context, _ *task.CreateTaskParam) (*task.Task, error) {
			return created, nil
		},
	})

	set := taskendpoint.New(svc)
	out, err := set.CreateTask(context.Background(), &task.CreateTaskParam{
		OfAccountID: 1,
		SourceURL:   "https://example.com/file.zip",
		FileName:    "file.zip",
		Checksum:    &task.ChecksumInfo{},
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(100), out.ID)
}

func TestSet_GetTask_RoundTrip(t *testing.T) {
	svc := &mockTaskService{
		getTaskFn: func(_ context.Context, id uint64) (*task.Task, error) {
			return stubTask(id), nil
		},
	}

	set := taskendpoint.New(svc)
	out, err := set.GetTask(context.Background(), 55)

	require.NoError(t, err)
	assert.Equal(t, uint64(55), out.ID)
}

func TestSet_ListTasks_RoundTrip(t *testing.T) {
	svc := &mockTaskService{
		listTasksFn: func(_ context.Context, _ *task.ListTasksParam) (*task.ListTasksOutput, error) {
			return &task.ListTasksOutput{
				Tasks: []*task.Task{stubTask(1), stubTask(2), stubTask(3)},
				Total: 3,
			}, nil
		},
	}

	set := taskendpoint.New(svc)
	out, err := set.ListTasks(context.Background(), &task.ListTasksParam{
		Filter: &task.TaskFilter{OfAccountID: 1},
		Limit:  10,
	})

	require.NoError(t, err)
	assert.Equal(t, int32(3), out.Total)
	assert.Len(t, out.Tasks, 3)
}

func TestSet_DeleteTask_RoundTrip(t *testing.T) {
	deleted := false
	svc := &mockTaskService{
		deleteTaskFn: func(_ context.Context, id uint64) error {
			assert.Equal(t, uint64(77), id)
			deleted = true
			return nil
		},
	}

	set := taskendpoint.New(svc)
	err := set.DeleteTask(context.Background(), 77)

	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestSet_PauseTask_InvalidState_RoundTrip(t *testing.T) {
	svc := &mockTaskService{
		pauseTaskFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{
				Code:    apperrors.ErrCodeInvalidInput,
				Message: "Only downloading tasks can be paused",
			}
		},
	}

	set := taskendpoint.New(svc)
	err := set.PauseTask(context.Background(), 1)

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
}

func TestSet_CancelTask_AlreadyCancelled_RoundTrip(t *testing.T) {
	svc := &mockTaskService{
		cancelTaskFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{
				Code:    apperrors.ErrCodeInvalidInput,
				Message: "Completed or cancelled tasks cannot be cancelled",
			}
		},
	}

	set := taskendpoint.New(svc)
	err := set.CancelTask(context.Background(), 2)

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
}

func TestSet_RetryTask_OnlyFailedAllowed_RoundTrip(t *testing.T) {
	svc := &mockTaskService{
		retryTaskFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{Code: apperrors.ErrCodeInvalidInput, Message: "Only failed tasks can be retried"}
		},
	}

	set := taskendpoint.New(svc)
	err := set.RetryTask(context.Background(), 3)

	require.Error(t, err)
	assert.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
}

func TestSet_CheckFileExists_RoundTrip(t *testing.T) {
	svc := &mockTaskService{
		checkFileExistsFn: func(_ context.Context, _ uint64) (bool, error) { return true, nil },
	}

	set := taskendpoint.New(svc)
	exists, err := set.CheckFileExists(context.Background(), 10)

	require.NoError(t, err)
	assert.True(t, exists)
}

func TestSet_UpdateTaskError_RoundTrip(t *testing.T) {
	svc := svcWithGet(&mockTaskService{
		updateErrorFn: func(_ context.Context, id uint64, err error) error {
			assert.Equal(t, uint64(30), id)
			return nil
		},
	})

	set := taskendpoint.New(svc)
	err := set.UpdateTaskError(context.Background(), 30, errors.New("something went wrong"))
	require.NoError(t, err)
}

func TestSet_CompleteTask_RoundTrip(t *testing.T) {
	svc := svcWithGet(&mockTaskService{
		completeTaskFn: func(_ context.Context, id uint64) error {
			assert.Equal(t, uint64(50), id)
			return nil
		},
	})

	set := taskendpoint.New(svc)
	err := set.CompleteTask(context.Background(), 50)
	require.NoError(t, err)
}

func mustNewStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	require.NoError(t, err)
	return s
}

func TestSet_GetTaskProgress_RoundTrip(t *testing.T) {
	svc := &mockTaskService{
		getTaskProgressFn: func(_ context.Context, id uint64) (*task.DownloadProgress, error) {
			return &task.DownloadProgress{TotalBytes: 4096, DownloadedBytes: 1024}, nil
		},
	}

	set := taskendpoint.New(svc)
	progress, err := set.GetTaskProgress(context.Background(), 60)

	require.NoError(t, err)
	require.NotNil(t, progress)
	assert.Equal(t, int64(4096), progress.TotalBytes)
}
