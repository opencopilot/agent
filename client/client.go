package main

import (
	"context"
	"fmt"
	"log"

	pb "github.com/opencopilot/agent/agent"
	"google.golang.org/grpc"
)

const (
	address = "localhost:50051"
)

func main() {
	// Set up a connection to the server.
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewAgentClient(conn)

	// r, err := c.StartService(context.Background(), &pb.StartServiceRequest{Image: "ghost"})
	r, err := c.GetStatus(context.Background(), &pb.StatusRequest{})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}

	for _, service := range r.Services {
		fmt.Printf("%s\n", service)
	}

}
