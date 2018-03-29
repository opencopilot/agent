package main

import (
	"context"
	"fmt"
	"log"

	pb "github.com/patrickdevivo/plumb-agent/protobuf/plumb"
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
	c := pb.NewPlumbAgentClient(conn)

	// r, err := c.StartService(context.Background(), &pb.StartServiceRequest{Image: "ghost"})
	r, err := c.GetStatus(context.Background(), &pb.StatusRequest{})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}

	for _, service := range r.Services {
		fmt.Printf("%s\n", service)
	}

}
