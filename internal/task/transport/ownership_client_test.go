package tasktransport

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ierrors "github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	pb "github.com/yuisofull/goload/internal/task/pb"
)

type fakeOwnershipRPC struct {
	pb.TaskOwnershipClient
	claimResp *pb.ClaimTaskResponse
	renewResp *pb.RenewLeaseResponse
	err       error

	gotRenew    *pb.RenewLeaseRequest
	gotComplete *pb.CompleteAttemptRequest
}

func (f *fakeOwnershipRPC) ClaimTask(_ context.Context, _ *pb.ClaimTaskRequest, _ ...grpc.CallOption) (*pb.ClaimTaskResponse, error) {
	return f.claimResp, f.err
}

func (f *fakeOwnershipRPC) RenewLease(_ context.Context, in *pb.RenewLeaseRequest, _ ...grpc.CallOption) (*pb.RenewLeaseResponse, error) {
	f.gotRenew = in
	return f.renewResp, f.err
}

func (f *fakeOwnershipRPC) CompleteAttempt(_ context.Context, in *pb.CompleteAttemptRequest, _ ...grpc.CallOption) (*pb.CompleteAttemptResponse, error) {
	f.gotComplete = in
	return &pb.CompleteAttemptResponse{}, f.err
}

func newTestClient(f pb.TaskOwnershipClient, now time.Time) *OwnershipClient {
	return &OwnershipClient{rpc: f, now: func() time.Time { return now }}
}

func TestClaimTaskDeadlineIsLocalSendTimePlusDuration(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	f := &fakeOwnershipRPC{claimResp: &pb.ClaimTaskResponse{Generation: 3, LeaseDurationMs: 60_000}}

	a, err := newTestClient(f, base).ClaimTask(context.Background(), 42, "w1")
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if a.Generation != 3 || a.TaskID != 42 || a.WorkerID != "w1" {
		t.Fatalf("unexpected attempt: %+v", a)
	}
	if want := base.Add(60 * time.Second); !a.LeaseExpiresAt.Equal(want) {
		t.Fatalf("deadline = %v, want %v (send time + granted duration)", a.LeaseExpiresAt, want)
	}
}

func TestClientRenewLeaseForwardsProgressAndStoring(t *testing.T) {
	base := time.Unix(2_000_000, 0)
	f := &fakeOwnershipRPC{renewResp: &pb.RenewLeaseResponse{LeaseDurationMs: 30_000}}

	expires, err := newTestClient(f, base).RenewLease(context.Background(), 7, "w1", 5, events.LeaseUpdate{
		Storing:  true,
		Progress: &events.AttemptProgress{Progress: 42.5, DownloadedBytes: 10, TotalBytes: 20},
	})
	if err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	if !f.gotRenew.GetStoring() || f.gotRenew.GetGeneration() != 5 {
		t.Fatalf("storing/generation not forwarded: %+v", f.gotRenew)
	}
	if p := f.gotRenew.GetProgress(); p == nil || p.GetProgress() != 42.5 || p.GetDownloadedBytes() != 10 {
		t.Fatalf("progress not forwarded: %+v", p)
	}
	if want := base.Add(30 * time.Second); !expires.Equal(want) {
		t.Fatalf("deadline = %v, want %v", expires, want)
	}
}

func TestClientCompleteAttemptMapsResult(t *testing.T) {
	f := &fakeOwnershipRPC{}
	err := newTestClient(f, time.Now()).CompleteAttempt(context.Background(), 1, "w1", 2, events.AttemptResult{
		FileName:    "f.iso",
		FileSize:    99,
		StorageType: "minio",
		StorageKey:  "1/2/f.iso",
		Checksum:    &events.ChecksumInfo{ChecksumType: "sha256", ChecksumValue: "abc"},
	})
	if err != nil {
		t.Fatalf("CompleteAttempt: %v", err)
	}
	r := f.gotComplete.GetResult()
	if r.GetFileName() != "f.iso" || r.GetStorageType() != "minio" || r.GetStorageKey() != "1/2/f.iso" {
		t.Fatalf("result not mapped: %+v", r)
	}
	if c := r.GetChecksum(); c == nil || c.GetChecksumValue() != "abc" {
		t.Fatalf("checksum not mapped: %+v", c)
	}
}

func TestSafeFailureMessageHidesInternalCause(t *testing.T) {
	typed := &ierrors.Error{Code: ierrors.ErrCodeInternal, Message: "failed to store file", Cause: errors.New("open /var/secret/private-path: denied")}
	if got := safeFailureMessage(typed); got != "failed to store file" {
		t.Fatalf("typed error sent %q, want the safe message only", got)
	}
	if got := safeFailureMessage(errors.New("dial tcp 10.0.0.5:5432: i/o timeout")); got != "download failed" {
		t.Fatalf("untyped error sent %q, want the generic message", got)
	}
	if got := safeFailureMessage(nil); got != "" {
		t.Fatalf("nil cause sent %q, want empty", got)
	}
}

func TestDecodeOwnershipErrorInvertsCodes(t *testing.T) {
	cases := []struct {
		code codes.Code
		want error
	}{
		{codes.FailedPrecondition, events.ErrTaskNotClaimable},
		{codes.Aborted, events.ErrAttemptRejected},
	}
	for _, tc := range cases {
		got := decodeOwnershipError(status.Error(tc.code, "x"))
		if !errors.Is(got, tc.want) {
			t.Fatalf("code %v decoded to %v, want errors.Is %v", tc.code, got, tc.want)
		}
	}
	if decodeOwnershipError(nil) != nil {
		t.Fatal("nil must decode to nil")
	}
	other := status.Error(codes.Unavailable, "down")
	if got := decodeOwnershipError(other); errors.Is(got, events.ErrAttemptRejected) || errors.Is(got, events.ErrTaskNotClaimable) {
		t.Fatalf("transient error must not decode to a sentinel: %v", got)
	}
}
