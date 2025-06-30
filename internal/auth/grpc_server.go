package auth

//
//import (
//	"context"
//	"net"
//
//	"github.com/yuisofull/goload/internal/auth/pb"
//	"google.golang.org/grpc"
//)
//
//// GRPCServer represents the auth gRPC server
//type GRPCServer interface {
//	Start(ctx context.Context) error
//	Stop() error
//}
//
//// grpcServerImpl implements the GRPCServer interface
//type grpcServerImpl struct {
//	server    *grpc.Server
//	listener  net.Listener
//	address   string
//	endpoints EndpointSet
//}
//
//// NewGRPCServer creates a new auth gRPC server
//func NewGRPCServer(address string, endpoints EndpointSet) GRPCServer {
//	return &grpcServerImpl{
//		address:   address,
//		endpoints: endpoints,
//	}
//}
//
//// Start starts the gRPC server
//func (s *grpcServerImpl) Start(ctx context.Context) error {
//	listener, err := net.Listen("tcp", s.address)
//	if err != nil {
//		return err
//	}
//	s.listener = listener
//
//	// Create gRPC server
//	s.server = grpc.NewServer()
//
//	// Create and register the auth service
//	authService := NewGRPCServer(s.endpoints)
//	pb.RegisterAuthServiceServer(s.server, authService)
//
//	// Start serving in a goroutine
//	go func() {
//		if err := s.server.Serve(listener); err != nil {
//			// Log error or handle it appropriately
//		}
//	}()
//
//	// Wait for context cancellation
//	<-ctx.Done()
//	return s.Stop()
//}
//
//// Stop stops the gRPC server gracefully
//func (s *grpcServerImpl) Stop() error {
//	if s.server != nil {
//		s.server.GracefulStop()
//	}
//	if s.listener != nil {
//		return s.listener.Close()
//	}
//	return nil
//}
