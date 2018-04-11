package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	pb "github.com/opencopilot/agent/agent"
	pbManager "github.com/opencopilot/agent/manager"
	"go.uber.org/zap"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags"

	dockerTypes "github.com/docker/docker/api/types"
	dockerClient "github.com/docker/docker/client"
	consulClient "github.com/hashicorp/consul/api"
)

const (
	port = ":50051"
)

var (
	// AgentID is the identifier of this agent/device
	AgentID = os.Getenv("AGENT_ID")
)

type server struct {
	dockerClient dockerClient.Client
	consulClient consulClient.Client
}

func (s *server) GetStatus(ctx context.Context, in *pb.AgentStatusRequest) (*pb.AgentStatus, error) {
	return AgentGetStatus(ctx)
}

// func (s *server) StartService(ctx context.Context, in *pb.StartServiceRequest) (*pb.AgentStatus, error) {
// 	reader, err := s.dockerClient.ImagePull(ctx, in.ImageRef, dockerTypes.ImagePullOptions{})
// 	_ = reader
// 	if err != nil {
// 		return nil, err
// 	}

// 	resp, err := s.dockerClient.ContainerCreate(ctx, &container.Config{
// 		Image: in.ImageRef,
// 	}, nil, nil, "")
// 	if err != nil {
// 		return nil, err
// 	}

// 	if err := s.dockerClient.ContainerStart(ctx, resp.ID, dockerTypes.ContainerStartOptions{}); err != nil {
// 		return nil, err
// 	}

// 	status, error := AgentGetStatus(ctx)
// 	return status, error
// }

// func (s *server) ConfigureService(ctx context.Context, in *pb.ConfigureServiceRequest) (*pb.AgentStatus, error) {

// 	kv := s.consulClient.KV()

// 	pair, _, err := kv.Get(AgentID+"/"+in.ContainerId, nil)
// 	if err != nil {
// 		return nil, err
// 	}

// 	if pair.Value != nil {
// 		fmt.Printf("KV: %s\n", pair.Value)
// 	}

// 	return AgentGetStatus(ctx)
// }

// func (s *server) StopService(ctx context.Context, in *pb.StopServiceRequest) (*pb.AgentStatus, error) {
// 	if err := s.dockerClient.ContainerStop(ctx, in.ContainerId, nil); err != nil {
// 		return nil, err
// 	}

// 	return AgentGetStatus(ctx)
// }

func (s *server) GetServiceLogs(in *pb.GetServiceLogsRequest, stream pb.Agent_GetServiceLogsServer) error {

	options := dockerTypes.ContainerLogsOptions{ShowStdout: true}
	out, err := s.dockerClient.ContainerLogs(context.Background(), in.ContainerId, options)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(out)

	for scanner.Scan() {
		t := scanner.Text()
		l := &pb.ServiceLogLine{Line: t}
		if err := stream.Send(l); err != nil {
			return err
		}
	}

	return nil
}

func (s *server) SetServiceGRPC(ctx context.Context, in *pb.SetServiceGRPCRequest) (*pb.AgentStatus, error) {
	kv := s.consulClient.KV()

	p := &consulClient.KVPair{Key: AgentID + "/" + in.ContainerId, Value: []byte(strconv.Itoa(int(in.Port)))}
	_, err := kv.Put(p, nil)
	if err != nil {
		return nil, err
	}

	return AgentGetStatus(ctx)
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
	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		log.Fatalf("failed to setup docker client on public gRPC server")
	}

	consulCli, err := consulClient.NewClient(consulClient.DefaultConfig())
	if err != nil {
		log.Fatalf("failed to setup consul client on public gRPC server")
	}

	pb.RegisterAgentServer(s, &server{
		dockerClient: *dockerCli,
		consulClient: *consulCli,
	})
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
	pb.RegisterAgentServer(s, &server{})

	// Register reflection service on gRPC server.
	reflection.Register(s)
	s.Serve(lis)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
func getManagerClient(port int) (pbManager.ManagerClient, *grpc.ClientConn) {
	conn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%d", port), grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	// defer conn.Close()
	return pbManager.NewManagerClient(conn), conn
}

func main() {
	if AgentID == "" {
		panic(errors.New("No agent ID specified"))
	}

	go servePublicGRPC()
	servePrivateGRPC()
}
