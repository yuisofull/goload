package authtransport

import (
	"context"
	"errors"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/transport"
	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/go-kit/log/level"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/yuisofull/goload/internal/auth"
	authendpoint "github.com/yuisofull/goload/internal/auth/endpoint"
	pb "github.com/yuisofull/goload/internal/auth/pb"
	internalerrors "github.com/yuisofull/goload/internal/errors"
)

type grpcServer struct {
	pb.UnimplementedAuthServiceServer

	createAccount grpctransport.Handler
	createSession grpctransport.Handler
	verifySession grpctransport.Handler
}

// CreateAccount implements the gRPC CreateAccount method
func (s *grpcServer) CreateAccount(
	ctx context.Context,
	req *pb.CreateAccountRequest,
) (*pb.CreateAccountResponse, error) {
	_, resp, err := s.createAccount.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.CreateAccountResponse), nil
}

// CreateSession implements the gRPC CreateSession method
func (s *grpcServer) CreateSession(
	ctx context.Context,
	req *pb.CreateSessionRequest,
) (*pb.CreateSessionResponse, error) {
	ctx, resp, err := s.createSession.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.CreateSessionResponse), nil
}

// VerifySession implements the gRPC VerifySession method
func (s *grpcServer) VerifySession(
	ctx context.Context,
	req *pb.VerifySessionRequest,
) (*pb.VerifySessionResponse, error) {
	_, resp, err := s.verifySession.ServeGRPC(ctx, req)
	if err != nil {
		return nil, encodeError(ctx, err)
	}
	return resp.(*pb.VerifySessionResponse), nil
}

func encodeError(_ context.Context, err error) error {
	var svcErr *internalerrors.Error
	if errors.As(err, &svcErr) {
		switch svcErr.Code {
		case auth.ErrCodeInvalidPassword:
			return status.Error(codes.Unauthenticated, svcErr.Message)
		case auth.ErrCodeInvalidToken:
			return status.Error(codes.Unauthenticated, svcErr.Message)
		default:
			// Delegate standard error codes (e.g. NOT_FOUND) to shared mapper.
			return internalerrors.EncodeGRPCError(svcErr)
		}
	}

	return internalerrors.EncodeGRPCError(err)
}

func NewGRPCServer(endpoints authendpoint.Set, logger log.Logger) pb.AuthServiceServer {
	options := []grpctransport.ServerOption{
		grpctransport.ServerErrorHandler(transport.NewLogErrorHandler(level.Error(logger))),
	}

	return &grpcServer{
		createAccount: grpctransport.NewServer(
			endpoints.CreateAccountEndpoint,
			decodeCreateAccountRequest,
			encodeCreateAccountResponse,
			options...,
		),
		createSession: grpctransport.NewServer(
			endpoints.CreateSessionEndpoint,
			decodeCreateSessionRequest,
			encodeCreateSessionResponse,
			options...,
		),
		verifySession: grpctransport.NewServer(
			endpoints.VerifyTokenEndpoint,
			decodeVerifySessionRequest,
			encodeVerifySessionResponse,
			options...,
		),
	}
}

func NewGRPCClient(conn *grpc.ClientConn, logger log.Logger) auth.Service {
	options := []grpctransport.ClientOption{
		grpctransport.ClientBefore(NewLogRequestFunc(logger)),
		grpctransport.ClientAfter(NewLogResponseFunc(logger)),
	}
	return &authendpoint.Set{
		CreateAccountEndpoint: grpctransport.NewClient(
			conn,
			"auth.v1.AuthService",
			"CreateAccount",
			encodeCreateAccountRequest,
			decodeCreateAccountResponse,
			pb.CreateAccountResponse{},
			options...,
		).Endpoint(),
		CreateSessionEndpoint: grpctransport.NewClient(
			conn,
			"auth.v1.AuthService",
			"CreateSession",
			encodeCreateSessionRequest,
			decodeCreateSessionResponse,
			pb.CreateSessionResponse{},
			options...,
		).Endpoint(),
		VerifyTokenEndpoint: grpctransport.NewClient(
			conn,
			"auth.v1.AuthService",
			"VerifySession",
			encodeVerifySessionRequest,
			decodeVerifySessionResponse,
			pb.VerifySessionResponse{},
			options...,
		).Endpoint(),
	}
}

// Server-side decode functions (protobuf -> endpoint types)

// decodeCreateAccountRequest converts protobuf CreateAccountRequest to endpoint CreateAccountRequest
func decodeCreateAccountRequest(_ context.Context, grpcReq any) (any, error) {
	req := grpcReq.(*pb.CreateAccountRequest)
	return &authendpoint.CreateAccountRequest{
		AccountName: req.GetAccountName(),
		Password:    req.GetPassword(),
	}, nil
}

// decodeCreateSessionRequest converts protobuf CreateSessionRequest to endpoint CreateSessionRequest
func decodeCreateSessionRequest(_ context.Context, grpcReq any) (any, error) {
	req := grpcReq.(*pb.CreateSessionRequest)
	return &authendpoint.CreateSessionRequest{
		AccountName: req.GetAccountName(),
		Password:    req.GetPassword(),
	}, nil
}

// decodeVerifySessionRequest converts protobuf VerifySessionRequest to endpoint VerifyTokenRequest
func decodeVerifySessionRequest(_ context.Context, grpcReq any) (any, error) {
	req := grpcReq.(*pb.VerifySessionRequest)
	return &authendpoint.VerifyTokenRequest{
		Token: req.GetToken(),
	}, nil
}

// Server-side encode functions (endpoint types -> protobuf)

// encodeCreateAccountResponse converts endpoint CreateAccountResponse to protobuf CreateAccountResponse
func encodeCreateAccountResponse(_ context.Context, response any) (any, error) {
	resp := response.(*authendpoint.CreateAccountResponse)
	return &pb.CreateAccountResponse{
		AccountId: resp.AccountId,
	}, nil
}

// encodeCreateSessionResponse converts endpoint CreateSessionResponse to protobuf CreateSessionResponse
func encodeCreateSessionResponse(_ context.Context, response any) (any, error) {
	resp := response.(*authendpoint.CreateSessionResponse)
	var pbAcct *pb.Account
	if resp.Account != nil {
		pbAcct = &pb.Account{
			Id:          resp.Account.GetId(),
			AccountName: resp.Account.GetAccountName(),
		}
	}
	return &pb.CreateSessionResponse{
		Token:   resp.Token,
		Account: pbAcct,
	}, nil
}

// encodeVerifySessionResponse converts endpoint VerifyTokenResponse to protobuf VerifySessionResponse
func encodeVerifySessionResponse(_ context.Context, response any) (any, error) {
	resp := response.(*authendpoint.VerifyTokenResponse)
	return &pb.VerifySessionResponse{
		AccountId: resp.AccountId,
	}, nil
}

// Client-side encode functions (endpoint types -> protobuf)

// encodeCreateAccountRequest converts endpoint CreateAccountRequest to protobuf CreateAccountRequest
func encodeCreateAccountRequest(_ context.Context, request any) (any, error) {
	req := request.(*authendpoint.CreateAccountRequest)
	return &pb.CreateAccountRequest{
		AccountName: req.AccountName,
		Password:    req.Password,
	}, nil
}

// encodeCreateSessionRequest converts endpoint CreateSessionRequest to protobuf CreateSessionRequest
func encodeCreateSessionRequest(_ context.Context, request any) (any, error) {
	req := request.(*authendpoint.CreateSessionRequest)
	return &pb.CreateSessionRequest{
		AccountName: req.AccountName,
		Password:    req.Password,
	}, nil
}

// encodeVerifySessionRequest converts endpoint VerifyTokenRequest to protobuf VerifySessionRequest
func encodeVerifySessionRequest(_ context.Context, request any) (any, error) {
	req := request.(*authendpoint.VerifyTokenRequest)
	return &pb.VerifySessionRequest{
		Token: req.Token,
	}, nil
}

// Client-side decode functions (protobuf -> endpoint types)

// decodeCreateAccountResponse converts protobuf CreateAccountResponse to endpoint CreateAccountResponse
func decodeCreateAccountResponse(_ context.Context, grpcResp any) (any, error) {
	resp := grpcResp.(*pb.CreateAccountResponse)
	return &authendpoint.CreateAccountResponse{
		AccountId: resp.GetAccountId(),
	}, nil
}

// decodeCreateSessionResponse converts protobuf CreateSessionResponse to endpoint CreateSessionResponse
func decodeCreateSessionResponse(_ context.Context, grpcResp any) (any, error) {
	resp := grpcResp.(*pb.CreateSessionResponse)
	var acct *pb.Account
	if resp.GetAccount() != nil {
		acct = &pb.Account{
			Id:          resp.GetAccount().GetId(),
			AccountName: resp.GetAccount().GetAccountName(),
		}
	}
	return &authendpoint.CreateSessionResponse{
		Token:   resp.GetToken(),
		Account: acct,
	}, nil
}

// decodeVerifySessionResponse converts protobuf VerifySessionResponse to endpoint VerifyTokenResponse
func decodeVerifySessionResponse(_ context.Context, grpcResp any) (any, error) {
	resp := grpcResp.(*pb.VerifySessionResponse)
	return &authendpoint.VerifyTokenResponse{
		AccountId: resp.GetAccountId(),
	}, nil
}
