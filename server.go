package main

import (
	"bufio"
	"context"

	pb "github.com/opencopilot/agent/agent"

	dockerTypes "github.com/docker/docker/api/types"
	docker "github.com/docker/docker/client"
	consul "github.com/hashicorp/consul/api"
)

type server struct {
	dockerCli *docker.Client
	consulCli *consul.Client
}

func (s *server) ToAgent() *Agent {
	return &Agent{
		dockerCli: s.dockerCli,
		consulCli: s.consulCli,
	}
}

func (s *server) Check(ctx context.Context, in *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	return &pb.HealthCheckResponse{
		Status: pb.HealthCheckResponse_SERVING,
	}, nil
}

func (s *server) GetStatus(ctx context.Context, in *pb.AgentStatusRequest) (*pb.AgentStatus, error) {
	agent := s.ToAgent()
	return agent.AgentGetStatus(ctx)
}

func (s *server) GetServiceLogs(in *pb.GetServiceLogsRequest, stream pb.Agent_GetServiceLogsServer) error {

	options := dockerTypes.ContainerLogsOptions{ShowStdout: true}
	out, err := s.dockerCli.ContainerLogs(context.Background(), in.ContainerId, options)
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
