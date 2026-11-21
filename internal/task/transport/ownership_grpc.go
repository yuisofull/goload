package tasktransport

import (
	"context"
	stderrors "errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
	pb "github.com/yuisofull/goload/internal/task/pb"
)

// AttemptService is the ownership API the TaskOwnership server exposes. The
// concrete *task.AttemptService satisfies it; tests supply a fake.
type AttemptService interface {
	ClaimTask(ctx context.Context, taskID uint64, workerID string) (*events.Attempt, error)
	RenewLease(ctx context.Context, taskID uint64, workerID string, generation uint64, update events.LeaseUpdate) (time.Time, error)
	CompleteAttempt(ctx context.Context, taskID uint64, workerID string, generation uint64, result events.AttemptResult) error
	FailAttempt(ctx context.Context, taskID uint64, workerID string, generation uint64, cause error) error
	ReleaseAttempt(ctx context.Context, taskID uint64, workerID string, generation uint64) error
}

const (
	codeNotClaimable   = codes.FailedPrecondition
	codeAttemptFenced  = codes.Aborted
	msgNotClaimable    = "task is not claimable"
	msgAttemptRejected = "attempt rejected"
)

func encodeAttemptError(err error) error {
	switch {
	case stderrors.Is(err, events.ErrAttemptRejected):
		return status.Error(codeAttemptFenced, msgAttemptRejected)
	case stderrors.Is(err, events.ErrTaskNotClaimable):
		return status.Error(codeNotClaimable, msgNotClaimable)
	}
	return errors.EncodeGRPCError(err)
}

func leaseMillis(expires time.Time) int64 {
	return time.Until(expires).Milliseconds()
}

type ownershipServer struct {
	pb.UnimplementedTaskOwnershipServer
	attempts AttemptService
}

func NewTaskOwnershipGRPCServer(attempts AttemptService) pb.TaskOwnershipServer {
	return &ownershipServer{attempts: attempts}
}

func (s *ownershipServer) ClaimTask(ctx context.Context, req *pb.ClaimTaskRequest) (*pb.ClaimTaskResponse, error) {
	a, err := s.attempts.ClaimTask(ctx, req.GetTaskId(), req.GetWorkerId())
	if err != nil {
		return nil, encodeAttemptError(err)
	}
	return &pb.ClaimTaskResponse{Generation: a.Generation, LeaseDurationMs: leaseMillis(a.LeaseExpiresAt)}, nil
}

func (s *ownershipServer) RenewLease(ctx context.Context, req *pb.RenewLeaseRequest) (*pb.RenewLeaseResponse, error) {
	update := events.LeaseUpdate{Storing: req.GetStoring()}
	if p := req.GetProgress(); p != nil {
		update.Progress = &events.AttemptProgress{
			Progress:        p.GetProgress(),
			DownloadedBytes: p.GetDownloadedBytes(),
			TotalBytes:      p.GetTotalBytes(),
		}
	}
	expires, err := s.attempts.RenewLease(ctx, req.GetTaskId(), req.GetWorkerId(), req.GetGeneration(), update)
	if err != nil {
		return nil, encodeAttemptError(err)
	}
	return &pb.RenewLeaseResponse{LeaseDurationMs: leaseMillis(expires)}, nil
}

func (s *ownershipServer) CompleteAttempt(ctx context.Context, req *pb.CompleteAttemptRequest) (*pb.CompleteAttemptResponse, error) {
	r := req.GetResult()
	result := events.AttemptResult{
		FileName:    r.GetFileName(),
		FileSize:    r.GetFileSize(),
		ContentType: r.GetContentType(),
		StorageType: storage.Type(r.GetStorageType()),
		StorageKey:  r.GetStorageKey(),
	}
	if c := r.GetChecksum(); c != nil {
		result.Checksum = &events.ChecksumInfo{ChecksumType: c.GetChecksumType(), ChecksumValue: c.GetChecksumValue()}
	}
	if err := s.attempts.CompleteAttempt(ctx, req.GetTaskId(), req.GetWorkerId(), req.GetGeneration(), result); err != nil {
		return nil, encodeAttemptError(err)
	}
	return &pb.CompleteAttemptResponse{}, nil
}

func (s *ownershipServer) FailAttempt(ctx context.Context, req *pb.FailAttemptRequest) (*pb.FailAttemptResponse, error) {
	if err := s.attempts.FailAttempt(ctx, req.GetTaskId(), req.GetWorkerId(), req.GetGeneration(), stderrors.New(req.GetCause())); err != nil {
		return nil, encodeAttemptError(err)
	}
	return &pb.FailAttemptResponse{}, nil
}

func (s *ownershipServer) ReleaseAttempt(ctx context.Context, req *pb.ReleaseAttemptRequest) (*pb.ReleaseAttemptResponse, error) {
	if err := s.attempts.ReleaseAttempt(ctx, req.GetTaskId(), req.GetWorkerId(), req.GetGeneration()); err != nil {
		return nil, encodeAttemptError(err)
	}
	return &pb.ReleaseAttemptResponse{}, nil
}
