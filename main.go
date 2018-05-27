package main

import (
	"errors"
	"log"
	"net"
	"os"
	"time"

	docker "github.com/docker/docker/client"
	consul "github.com/hashicorp/consul/api"
	pb "github.com/opencopilot/agent/agent"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags"
)

var (
	// InstanceID is the identifier of this agent/device
	InstanceID = os.Getenv("INSTANCE_ID")
	// ConfigDir is the config directory of opencopilot on the host
	ConfigDir = os.Getenv("CONFIG_DIR")
)

const (
	port = 50051
)

func servePublicGRPC(server *server) {
	lis, err := net.Listen("tcp", ":"+string(port))
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

	pb.RegisterAgentServer(s, server)
	// Register reflection service on gRPC server.
	reflection.Register(s)
	s.Serve(lis)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func servePrivateGRPC(server *server) {
	lis, err := net.Listen("tcp", "127.0.0.1:50050")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterAgentServer(s, server)

	// Register reflection service on gRPC server.
	reflection.Register(s)
	s.Serve(lis)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func watchConfigTree(agent *Agent, queue chan consul.KVPairs) {
	kv := agent.consulCli.KV()
	var prevIndex uint64
	for {
		kvs, queryMeta, err := kv.List("instances/"+InstanceID+"/services/", &consul.QueryOptions{
			WaitIndex: prevIndex,
		})
		if err != nil {
			log.Fatal(err)
		}
		lastIndex := queryMeta.LastIndex
		if prevIndex != lastIndex {
			queue <- kvs
			prevIndex = lastIndex
		}
	}
}

func pollConfigTree(agent *Agent, queue chan consul.KVPairs, interval time.Duration) {
	kv := agent.consulCli.KV()
	for {
		kvs, _, err := kv.List("instances/"+InstanceID+"/services", nil)
		if err != nil {
			log.Fatal(err)
		}
		select {
		case queue <- kvs:
		default:
			// something is already in the queue, lets not add to
		}
		time.Sleep(interval)
	}
}

func registerService(consulCli *consul.Client) {
	agent := consulCli.Agent()
	err := agent.ServiceRegister(&consul.AgentServiceRegistration{
		ID:   InstanceID,
		Name: "OpenCoPilot Agent",
		Port: port,
	})
	if err != nil {
		log.Panic(err)
	}
}

func main() {
	if InstanceID == "" {
		panic(errors.New("No instance ID specified"))
	}

	consulClientConfig := consul.DefaultConfig()
	if os.Getenv("ENV") == "dev" {
		consulClientConfig.Address = "host.docker.internal:8500"
	}

	consulCli, err := consul.NewClient(consulClientConfig)
	if err != nil {
		log.Fatalf("failed to initialize consul client")
	}

	dockerCli, err := docker.NewEnvClient()
	if err != nil {
		log.Fatalf("failed to initialize docker client")
	}

	server := &server{
		dockerCli: dockerCli,
		consulCli: consulCli,
	}

	agent := server.ToAgent()
	queue := make(chan consul.KVPairs, 1)

	log.Println("starting to watch Consul KV...")
	go watchConfigTree(agent, queue)

	log.Println("starting public gRPC...")
	go servePublicGRPC(server)

	log.Println("starting private gRPC...")
	go servePrivateGRPC(server)

	log.Println("registering service...")
	registerService(consulCli)

	log.Println("starting to poll Consul KV...")
	interval, _ := time.ParseDuration("15s") // Move this to an ENV var?
	go pollConfigTree(agent, queue, interval)

	log.Println("starting config handler...")
	agent.startConfigHandler(queue)
}
