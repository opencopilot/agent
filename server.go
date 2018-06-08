package main

import (
	"bufio"
	"context"

	pb "github.com/opencopilot/agent/agent"
	pbHealth "github.com/opencopilot/agent/health"

	dockerTypes "github.com/docker/docker/api/types"
	docker "github.com/docker/docker/client"
	consul "github.com/hashicorp/consul/api"
)

type server struct {
	dockerCli *docker.Client
	consulCli *consul.Client
}

type health struct{}

func (s *server) ToAgent() *Agent {
	return &Agent{
		dockerCli: s.dockerCli,
		consulCli: s.consulCli,
	}
}

func (s *server) Check(ctx context.Context, in *pbHealth.HealthCheckRequest) (*pbHealth.HealthCheckResponse, error) {
	return &pbHealth.HealthCheckResponse{
		Status: pbHealth.HealthCheckResponse_SERVING,
	}, nil
}

func (s *server) GetStatus(ctx context.Context, in *pb.AgentStatusRequest) (*pb.AgentStatus, error) {
	agent := s.ToAgent()
	return agent.AgentGetStatus(ctx)
}

func (s *server) GetServiceLogs(in *pb.GetServiceLogsRequest, stream pb.Agent_GetServiceLogsServer) error {
	options := dockerTypes.ContainerLogsOptions{ShowStderr: true}
	out, err := s.dockerCli.ContainerLogs(context.Background(), in.ContainerId, options)
	if err != nil {
		return err
	}
	defer out.Close()

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
