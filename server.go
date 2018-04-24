package main

import (
	"bufio"
	"context"
	"strconv"

	pb "github.com/opencopilot/agent/agent"

	dockerTypes "github.com/docker/docker/api/types"
	docker "github.com/docker/docker/client"
	consul "github.com/hashicorp/consul/api"
)

type server struct {
	dockerClient docker.Client
	consulClient consul.Client
}

func (s *server) GetStatus(ctx context.Context, in *pb.AgentStatusRequest) (*pb.AgentStatus, error) {
	return AgentGetStatus(ctx)
}

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

	p := &consul.KVPair{Key: InstanceID + "/" + in.ContainerId, Value: []byte(strconv.Itoa(int(in.Port)))}
	_, err := kv.Put(p, nil)
	if err != nil {
		return nil, err
	}

	return AgentGetStatus(ctx)
}
