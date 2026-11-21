package tasktransport

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
	pb "github.com/yuisofull/goload/internal/task/pb"
)

type fakeAttempts struct {
	claimErr, renewErr, completeErr, failErr, releaseErr error

	expires time.Time

	gotWorker     string
	gotGeneration uint64
	gotUpdate     events.LeaseUpdate
	gotResult     events.AttemptResult
	gotCause      error
	released      bool
}

func (f *fakeAttempts) ClaimTask(_ context.Context, id uint64, w string) (*events.Attempt, error) {
	f.gotWorker = w
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	return &events.Attempt{TaskID: id, WorkerID: w, Generation: 7, LeaseExpiresAt: f.expires}, nil
}

func (f *fakeAttempts) RenewLease(
	_ context.Context, _ uint64, w string, g uint64, u events.LeaseUpdate,
) (time.Time, error) {
	f.gotWorker, f.gotGeneration, f.gotUpdate = w, g, u
	return f.expires, f.renewErr
}

func (f *fakeAttempts) CompleteAttempt(
	_ context.Context, _ uint64, w string, g uint64, r events.AttemptResult,
) error {
	f.gotWorker, f.gotGeneration, f.gotResult = w, g, r
	return f.completeErr
}

func (f *fakeAttempts) FailAttempt(_ context.Context, _ uint64, w string, g uint64, cause error) error {
	f.gotWorker, f.gotGeneration, f.gotCause = w, g, cause
	return f.failErr
}

func (f *fakeAttempts) ReleaseAttempt(_ context.Context, _ uint64, w string, g uint64) error {
	f.gotWorker, f.gotGeneration, f.released = w, g, true
	return f.releaseErr
}

func newAttemptServer(f *fakeAttempts) pb.TaskOwnershipServer {
	return NewTaskOwnershipGRPCServer(f)
}

func TestClaimTaskReturnsGenerationAndDuration(t *testing.T) {
	f := &fakeAttempts{expires: time.Now().Add(60 * time.Second)}
	resp, err := newAttemptServer(f).ClaimTask(context.Background(), &pb.ClaimTaskRequest{TaskId: 1, WorkerId: "w1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Generation != 7 {
		t.Fatalf("generation = %d", resp.Generation)
	}
	if resp.LeaseDurationMs <= 50_000 || resp.LeaseDurationMs > 60_000 {
		t.Fatalf("lease_duration_ms = %d", resp.LeaseDurationMs)
	}
	if f.gotWorker != "w1" {
		t.Fatalf("worker = %q", f.gotWorker)
	}
}

func TestClaimTaskNotClaimableMapsToFailedPrecondition(t *testing.T) {
	f := &fakeAttempts{claimErr: events.ErrTaskNotClaimable}
	_, err := newAttemptServer(f).ClaimTask(context.Background(), &pb.ClaimTaskRequest{TaskId: 1, WorkerId: "w1"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, err = %v", status.Code(err), err)
	}
}

func TestRenewLeaseForwardsProgressAndStoring(t *testing.T) {
	f := &fakeAttempts{expires: time.Now().Add(30 * time.Second)}
	resp, err := newAttemptServer(f).RenewLease(context.Background(), &pb.RenewLeaseRequest{
		TaskId: 1, WorkerId: "w1", Generation: 3, Storing: true,
		Progress: &pb.AttemptProgress{Progress: 42.5, DownloadedBytes: 10, TotalBytes: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.LeaseDurationMs <= 0 {
		t.Fatalf("lease_duration_ms = %d", resp.LeaseDurationMs)
	}
	want := events.AttemptProgress{Progress: 42.5, DownloadedBytes: 10, TotalBytes: 20}
	if f.gotGeneration != 3 || !f.gotUpdate.Storing || f.gotUpdate.Progress == nil || *f.gotUpdate.Progress != want {
		t.Fatalf("forwarded: gen=%d update=%+v", f.gotGeneration, f.gotUpdate)
	}

	if _, err = newAttemptServer(f).RenewLease(context.Background(), &pb.RenewLeaseRequest{TaskId: 1, WorkerId: "w1"}); err != nil {
		t.Fatal(err)
	}
	if f.gotUpdate.Progress != nil || f.gotUpdate.Storing {
		t.Fatalf("absent progress must stay nil: %+v", f.gotUpdate)
	}
}

func TestFencedRejectionsMapToAborted(t *testing.T) {
	rej := events.ErrAttemptRejected
	f := &fakeAttempts{renewErr: rej, completeErr: rej, failErr: rej, releaseErr: rej}
	s := newAttemptServer(f)
	ctx := context.Background()

	_, e1 := s.RenewLease(ctx, &pb.RenewLeaseRequest{TaskId: 1, WorkerId: "w"})
	_, e2 := s.CompleteAttempt(ctx, &pb.CompleteAttemptRequest{TaskId: 1, WorkerId: "w", Result: &pb.AttemptResultMsg{}})
	_, e3 := s.FailAttempt(ctx, &pb.FailAttemptRequest{TaskId: 1, WorkerId: "w"})
	_, e4 := s.ReleaseAttempt(ctx, &pb.ReleaseAttemptRequest{TaskId: 1, WorkerId: "w"})
	for i, err := range []error{e1, e2, e3, e4} {
		if status.Code(err) != codes.Aborted {
			t.Fatalf("rpc %d: code = %v, err = %v", i, status.Code(err), err)
		}
	}
}

func TestCompleteAttemptMapsResult(t *testing.T) {
	f := &fakeAttempts{}
	s := newAttemptServer(f)
	_, err := s.CompleteAttempt(context.Background(), &pb.CompleteAttemptRequest{
		TaskId: 1, WorkerId: "w1", Generation: 4,
		Result: &pb.AttemptResultMsg{
			FileName: "a.bin", FileSize: 99, ContentType: "text/plain",
			Checksum:    &pb.AttemptChecksum{ChecksumType: "sha256", ChecksumValue: "abc"},
			StorageType: "minio", StorageKey: "k/4",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := f.gotResult
	if f.gotGeneration != 4 || r.FileName != "a.bin" || r.FileSize != 99 || r.ContentType != "text/plain" ||
		r.StorageType != storage.Type("minio") || r.StorageKey != "k/4" ||
		r.Checksum == nil || *r.Checksum != (events.ChecksumInfo{ChecksumType: "sha256", ChecksumValue: "abc"}) {
		t.Fatalf("result = %+v gen=%d", r, f.gotGeneration)
	}

	_, err = s.CompleteAttempt(context.Background(), &pb.CompleteAttemptRequest{
		TaskId: 1, WorkerId: "w1", Generation: 4, Result: &pb.AttemptResultMsg{StorageType: "local", StorageKey: "k"},
	})
	if err != nil || f.gotResult.Checksum != nil {
		t.Fatalf("absent checksum must stay nil: %v %+v", err, f.gotResult)
	}
}

func TestFailAndReleaseForward(t *testing.T) {
	f := &fakeAttempts{}
	s := newAttemptServer(f)
	if _, err := s.FailAttempt(context.Background(), &pb.FailAttemptRequest{
		TaskId: 1, WorkerId: "w1", Generation: 2, Cause: "boom",
	}); err != nil {
		t.Fatal(err)
	}
	if f.gotCause == nil || f.gotCause.Error() != "boom" || f.gotGeneration != 2 {
		t.Fatalf("cause=%v gen=%d", f.gotCause, f.gotGeneration)
	}
	if _, err := s.ReleaseAttempt(context.Background(), &pb.ReleaseAttemptRequest{
		TaskId: 1, WorkerId: "w1", Generation: 5,
	}); err != nil || !f.released || f.gotGeneration != 5 {
		t.Fatalf("release: %v %v %d", err, f.released, f.gotGeneration)
	}
}

func TestUnknownErrorIsClientSafe(t *testing.T) {
	f := &fakeAttempts{renewErr: errors.New("sql: connection to 10.0.0.5 refused")}
	_, err := newAttemptServer(f).RenewLease(context.Background(), &pb.RenewLeaseRequest{TaskId: 1, WorkerId: "w"})
	if err == nil || status.Code(err) == codes.OK {
		t.Fatal("want error")
	}
	if msg := status.Convert(err).Message(); msg != "internal server error" {
		t.Fatalf("leaked: %q", msg)
	}
}
