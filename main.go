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
	// HandlingServices is a global indicator of whether the ConfigHandler method is running
	HandlingServices = false // this feels hacky...
)

const (
	port = ":50051"
)

func servePublicGRPC(server *server) {
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

// TODO: use a loop here instead of a recursive call, for debugging purposes
func watchConfigTree(agent *Agent, prevIndex uint64, handler func(consul.KVPairs)) error {
	kv := agent.consulCli.KV()
	kvs, queryMeta, err := kv.List("instances/"+InstanceID+"/services/", &consul.QueryOptions{
		WaitIndex: prevIndex,
	})
	if err != nil {
		return err
	}
	lastIndex := queryMeta.LastIndex
	if prevIndex != lastIndex {
		handler(kvs)
	}
	watchConfigTree(agent, lastIndex, handler)
	return nil
}

func pollConfigTree(agent *Agent, interval time.Duration) {
	time.Sleep(interval) // Give the watchConfigTree time to handle the "first" config change (on first boot of agent)
	kv := agent.consulCli.KV()
	for {
		if !HandlingServices {
			kvs, _, err := kv.List("instances/"+InstanceID+"/services", nil)
			if err != nil {
				log.Fatal(err)
			}
			agent.ConfigHandler(kvs)
		}
		time.Sleep(interval)
	}
}

func main() {
	if InstanceID == "" {
		panic(errors.New("No instance ID specified"))
	}

	consulCli, err := consul.NewClient(consul.DefaultConfig())
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

	log.Println("Starting to watch Consul KV...")
	go watchConfigTree(agent, 0, agent.ConfigHandler)

	log.Println("Starting public gRPC...")
	go servePublicGRPC(server)

	log.Println("Starting private gRPC...")
	go servePrivateGRPC(server)

	// TODO: think about how to handle this properly
	log.Println("Starting to poll Consul KV...")
	interval, _ := time.ParseDuration("15s")
	pollConfigTree(agent, interval)
}
