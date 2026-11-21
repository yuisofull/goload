package tasktransport

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	pb "github.com/yuisofull/goload/internal/task/pb"
)

type OwnershipClient struct {
	rpc pb.TaskOwnershipClient
	now func() time.Time
}

func NewTaskOwnershipGRPCClient(conn *grpc.ClientConn) *OwnershipClient {
	return &OwnershipClient{rpc: pb.NewTaskOwnershipClient(conn), now: time.Now}
}

// deadline derives an absolute lease expiry from the local clock captured
// before the call, so the worker never compares its lease to a server clock.
func (c *OwnershipClient) deadline(sent time.Time, durationMs int64) time.Time {
	return sent.Add(time.Duration(durationMs) * time.Millisecond)
}

func (c *OwnershipClient) ClaimTask(ctx context.Context, taskID uint64, workerID string) (*events.Attempt, error) {
	sent := c.now()
	resp, err := c.rpc.ClaimTask(ctx, &pb.ClaimTaskRequest{TaskId: taskID, WorkerId: workerID})
	if err != nil {
		return nil, decodeOwnershipError(err)
	}
	return &events.Attempt{
		TaskID:         taskID,
		WorkerID:       workerID,
		Generation:     resp.GetGeneration(),
		LeaseExpiresAt: c.deadline(sent, resp.GetLeaseDurationMs()),
	}, nil
}

func (c *OwnershipClient) RenewLease(
	ctx context.Context,
	taskID uint64,
	workerID string,
	generation uint64,
	update events.LeaseUpdate,
) (time.Time, error) {
	req := &pb.RenewLeaseRequest{
		TaskId:     taskID,
		WorkerId:   workerID,
		Generation: generation,
		Storing:    update.Storing,
	}
	if p := update.Progress; p != nil {
		req.Progress = &pb.AttemptProgress{
			Progress:        p.Progress,
			DownloadedBytes: p.DownloadedBytes,
			TotalBytes:      p.TotalBytes,
		}
	}
	sent := c.now()
	resp, err := c.rpc.RenewLease(ctx, req)
	if err != nil {
		return time.Time{}, decodeOwnershipError(err)
	}
	return c.deadline(sent, resp.GetLeaseDurationMs()), nil
}

func (c *OwnershipClient) CompleteAttempt(
	ctx context.Context,
	taskID uint64,
	workerID string,
	generation uint64,
	result events.AttemptResult,
) error {
	msg := &pb.AttemptResultMsg{
		FileName:    result.FileName,
		FileSize:    result.FileSize,
		ContentType: result.ContentType,
		StorageType: string(result.StorageType),
		StorageKey:  result.StorageKey,
	}
	if c := result.Checksum; c != nil {
		msg.Checksum = &pb.AttemptChecksum{ChecksumType: c.ChecksumType, ChecksumValue: c.ChecksumValue}
	}
	_, err := c.rpc.CompleteAttempt(ctx, &pb.CompleteAttemptRequest{
		TaskId:     taskID,
		WorkerId:   workerID,
		Generation: generation,
		Result:     msg,
	})
	return decodeOwnershipError(err)
}

func (c *OwnershipClient) FailAttempt(ctx context.Context, taskID uint64, workerID string, generation uint64, cause error) error {
	_, err := c.rpc.FailAttempt(ctx, &pb.FailAttemptRequest{
		TaskId:     taskID,
		WorkerId:   workerID,
		Generation: generation,
		Cause:      safeFailureMessage(cause),
	})
	return decodeOwnershipError(err)
}

func (c *OwnershipClient) ReleaseAttempt(ctx context.Context, taskID uint64, workerID string, generation uint64) error {
	_, err := c.rpc.ReleaseAttempt(ctx, &pb.ReleaseAttemptRequest{
		TaskId:     taskID,
		WorkerId:   workerID,
		Generation: generation,
	})
	return decodeOwnershipError(err)
}

// safeFailureMessage keeps an adapter error's internal detail (paths, SQL) off
// the wire and out of the task's stored ErrorMessage (ERR-6).
func safeFailureMessage(cause error) string {
	if cause == nil {
		return ""
	}
	if e := errors.AsError(cause); e != nil && e.Message != "" {
		return e.Message
	}
	return "download failed"
}

func decodeOwnershipError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codeNotClaimable:
		return fmt.Errorf("%w: %v", events.ErrTaskNotClaimable, err)
	case codeAttemptFenced:
		return fmt.Errorf("%w: %v", events.ErrAttemptRejected, err)
	case codes.NotFound:
		// A deleted task: the worker acks and drops the dispatch, not retries.
		return &errors.Error{Code: errors.ErrCodeNotFound, Message: status.Convert(err).Message()}
	}
	return err
}
