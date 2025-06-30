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

type Set struct {
	CreateAccountEndpoint endpoint.Endpoint
	CreateSessionEndpoint endpoint.Endpoint
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

	return Set{
		CreateAccountEndpoint: createAccountEndpoint,
		CreateSessionEndpoint: createSessionEndpoint,
	}
}

func (e *Set) CreateAccount(ctx context.Context, params auth.CreateAccountParams) (auth.CreateAccountOutput, error) {
	out, err := e.CreateAccountEndpoint(ctx, &CreateAccountRequest{
		AccountName: params.AccountName,
		Password:    params.Password,
	})

	return out.(auth.CreateAccountOutput), err
}

func (e *Set) CreateSession(ctx context.Context, params auth.CreateSessionParams) (auth.CreateSessionOutput, error) {
	out, err := e.CreateSessionEndpoint(ctx, &CreateSessionRequest{
		AccountName: params.AccountName,
		Password:    params.Password,
	})

	return out.(auth.CreateSessionOutput), err
}
