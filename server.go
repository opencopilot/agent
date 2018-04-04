package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"

	pb "github.com/opencopilot/ocp-agent/protobuf/OpenCoPilot"
	"go.uber.org/zap"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags"
)

const (
	port = ":50051"
)

var (
	// AgentID is the identifier of this agent/device
	AgentID = os.Getenv("AGENT_ID")
)

type server struct{}

func (s *server) GetStatus(ctx context.Context, in *pb.StatusRequest) (*pb.Status, error) {
	return AgentGetStatus(ctx)
}

func (s *server) StartService(ctx context.Context, in *pb.StartServiceRequest) (*pb.Status, error) {
	return AgentStartService(ctx, in.ImageRef)
}

func (s *server) ConfigureService(ctx context.Context, in *pb.ConfigureServiceRequest) (*pb.Status, error) {
	return AgentConfigureService(ctx, in.ContainerId)
}

func (s *server) StopService(ctx context.Context, in *pb.StopServiceRequest) (*pb.Status, error) {
	return AgentStopService(ctx, in.ContainerId)
}

func (s *server) GetServiceLogs(in *pb.GetServiceLogsRequest, stream pb.OCPAgent_GetServiceLogsServer) error {
	return AgentGetServiceLogs(in.ContainerId, stream)
}

func (s *server) SetServiceGRPC(ctx context.Context, in *pb.SetServiceGRPCRequest) (*pb.Status, error) {
	return AgentSetServiceGRPC(ctx, in.ContainerId, in.Port)
}

func servePublicGRPC() {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	logger, err := zap.NewProduction()
	defer logger.Sync()
	if err != nil {
		log.Fatalf("failed to setup logger: %v", err)
	}

	// TODO: TLS for gRPC connection to outside world
	// creds, err := credentials.NewServerTLSFromFile("server.crt", "server.key")
	// if err != nil {
	// 	log.Fatalf("failed to load credentials: %v", err)
	// }

	s := grpc.NewServer(
		// grpc.Creds(creds),
		grpc.StreamInterceptor(grpc_middleware.ChainStreamServer(
			grpc_ctxtags.StreamServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_zap.StreamServerInterceptor(logger),
			grpc_recovery.StreamServerInterceptor(),
		)),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpc_ctxtags.UnaryServerInterceptor(grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.CodeGenRequestFieldExtractor)),
			grpc_zap.UnaryServerInterceptor(logger),
			grpc_recovery.UnaryServerInterceptor(),
		)),
	)
	pb.RegisterOCPAgentServer(s, &server{})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	s.Serve(lis)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func servePrivateGRPC() {
	lis, err := net.Listen("tcp", "127.0.0.1:50050")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterOCPAgentServer(s, &server{})

	// Register reflection service on gRPC server.
	reflection.Register(s)
	s.Serve(lis)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func main() {
	if AgentID == "" {
		panic(errors.New("No agent ID specified"))
	}

	go servePublicGRPC()
	servePrivateGRPC()
}
