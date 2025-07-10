package authendpoint

import (
	"context"
	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/ratelimit"
	"github.com/yuisofull/goload/internal/auth"
	pb "github.com/yuisofull/goload/internal/auth/pb"
	"golang.org/x/time/rate"
)

type CreateAccountRequest pb.CreateAccountRequest

type CreateAccountResponse pb.CreateAccountResponse

type CreateSessionRequest pb.CreateSessionRequest

type CreateSessionResponse pb.CreateSessionResponse

type VerifyTokenRequest pb.VerifySessionRequest
type VerifyTokenResponse pb.VerifySessionResponse

type Set struct {
	CreateAccountEndpoint endpoint.Endpoint
	CreateSessionEndpoint endpoint.Endpoint
	VerifyTokenEndpoint   endpoint.Endpoint
}

// MakeCreateAccountEndpoint creates an endpoint for the CreateAccount service method
func MakeCreateAccountEndpoint(svc auth.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateAccountRequest)

		params := auth.CreateAccountParams{
			AccountName: req.AccountName,
			Password:    req.Password,
		}

		output, err := svc.CreateAccount(ctx, params)
		if err != nil {
			return nil, err
		}

		return &CreateAccountResponse{
			AccountId: output.ID,
		}, nil
	}
}

// MakeCreateSessionEndpoint creates an endpoint for the CreateSession service method
func MakeCreateSessionEndpoint(svc auth.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*CreateSessionRequest)

		params := auth.CreateSessionParams{
			AccountName: req.AccountName,
			Password:    req.Password,
		}

		output, err := svc.CreateSession(ctx, params)
		if err != nil {
			return nil, err
		}

		return &CreateSessionResponse{
			Token: output.Token,
		}, nil
	}
}

// MakeVerifyTokenEndpoint creates an endpoint for the VerifySession service method
func MakeVerifyTokenEndpoint(svc auth.Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(*VerifyTokenRequest)
		params := auth.VerifySessionParams{
			Token: req.Token,
		}
		output, err := svc.VerifySession(ctx, params)
		if err != nil {
			return nil, err
		}
		return &VerifyTokenResponse{
			AccountId: output.AccountID,
		}, nil
	}
}

// New creates a new EndpointSet with all endpoints initialized
func New(svc auth.Service) Set {
	var createAccountEndpoint endpoint.Endpoint

	{
		createAccountEndpoint = MakeCreateAccountEndpoint(svc)
		createAccountEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Limit(1), 100))(createAccountEndpoint)
	}

	var createSessionEndpoint endpoint.Endpoint
	{
		createSessionEndpoint = MakeCreateSessionEndpoint(svc)
		createSessionEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Limit(1), 100))(createSessionEndpoint)
	}

	var verifyTokenEndpoint endpoint.Endpoint
	{
		verifyTokenEndpoint = MakeVerifyTokenEndpoint(svc)
		verifyTokenEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Limit(1), 100))(verifyTokenEndpoint)
	}

	return Set{
		CreateAccountEndpoint: createAccountEndpoint,
		CreateSessionEndpoint: createSessionEndpoint,
		VerifyTokenEndpoint:   verifyTokenEndpoint,
	}
}

func (e *Set) CreateAccount(ctx context.Context, params auth.CreateAccountParams) (auth.CreateAccountOutput, error) {
	resp, err := e.CreateAccountEndpoint(ctx, &CreateAccountRequest{
		AccountName: params.AccountName,
		Password:    params.Password,
	})
	if err != nil {
		return auth.CreateAccountOutput{}, err
	}
	out := resp.(*CreateAccountResponse)

	return auth.CreateAccountOutput{
		ID:          out.AccountId,
		AccountName: params.AccountName,
	}, nil
}

func (e *Set) CreateSession(ctx context.Context, params auth.CreateSessionParams) (auth.CreateSessionOutput, error) {
	resp, err := e.CreateSessionEndpoint(ctx, &CreateSessionRequest{
		AccountName: params.AccountName,
		Password:    params.Password,
	})
	if err != nil {
		return auth.CreateSessionOutput{}, err
	}
	out := resp.(*CreateSessionResponse)

	return auth.CreateSessionOutput{
		Token: out.Token,
		Account: &auth.Account{
			Id:          out.Account.Id,
			AccountName: out.Account.AccountName,
		},
	}, nil
}

func (e *Set) VerifySession(ctx context.Context, params auth.VerifySessionParams) (auth.VerifySessionOutput, error) {
	resp, err := e.VerifyTokenEndpoint(ctx, &VerifyTokenRequest{
		Token: params.Token,
	})
	if err != nil {
		return auth.VerifySessionOutput{}, err
	}
	out := resp.(*VerifyTokenResponse)
	return auth.VerifySessionOutput{
		AccountID: out.AccountId,
	}, nil
}
