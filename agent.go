package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"strconv"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockerClient "github.com/docker/docker/client"
	consulClient "github.com/hashicorp/consul/api"
	pb "github.com/opencopilot/ocp-agent/protobuf/OpenCoPilot"
)

// AgentGetStatus returns the status of a running service
func AgentGetStatus(ctx context.Context) (*pb.Status, error) {
	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		return nil, err
	}
	status := &pb.Status{AgentId: AgentID, Services: []*pb.Status_AgentService{}}

	containers, err := dockerCli.ContainerList(ctx, dockerTypes.ContainerListOptions{})
	if err != nil {
		log.Fatal(err)
	}

	for _, container := range containers {
		status.Services = append(status.Services, &pb.Status_AgentService{Id: container.ID, Image: container.Image})
	}

	return status, nil
}

// AgentStartService starts a new service
func AgentStartService(ctx context.Context, imageRef string) (*pb.Status, error) {
	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		return nil, err
	}
	reader, err := dockerCli.ImagePull(ctx, imageRef, dockerTypes.ImagePullOptions{})
	_ = reader
	if err != nil {
		return nil, err
	}

	resp, err := dockerCli.ContainerCreate(ctx, &container.Config{
		Image: imageRef,
	}, nil, nil, "")
	if err != nil {
		return nil, err
	}

	if err := dockerCli.ContainerStart(ctx, resp.ID, dockerTypes.ContainerStartOptions{}); err != nil {
		return nil, err
	}

	status, error := AgentGetStatus(ctx)
	return status, error
}

// AgentConfigureService tells a running service to re-configure itself
func AgentConfigureService(ctx context.Context, containerID string) (*pb.Status, error) {
	consulCli, err := consulClient.NewClient(consulClient.DefaultConfig())
	if err != nil {
		return nil, err
	}

	kv := consulCli.KV()

	pair, _, err := kv.Get(AgentID+"/"+containerID, nil)
	if err != nil {
		return nil, err
	}

	if pair.Value != nil {
		fmt.Printf("KV: %s\n", pair.Value)
	}

	return AgentGetStatus(ctx)
}

// AgentStopService stops a running service
func AgentStopService(ctx context.Context, containerID string) (*pb.Status, error) {
	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		return nil, err
	}
	if err := dockerCli.ContainerStop(ctx, containerID, nil); err != nil {
		return nil, err
	}

	return AgentGetStatus(ctx)
}

// AgentGetServiceLogs returns a stream of logs from a container. TODO: add parameters for limiting how many log lines to return
func AgentGetServiceLogs(containerID string, stream pb.OCPAgent_GetServiceLogsServer) error {
	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		return err
	}

	options := dockerTypes.ContainerLogsOptions{ShowStdout: true}
	out, err := dockerCli.ContainerLogs(context.Background(), containerID, options)
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

// AgentSetServiceGRPC sets the port that a running service can be reached on via gRPC
func AgentSetServiceGRPC(ctx context.Context, containerID string, port int32) (*pb.Status, error) {
	consulCli, err := consulClient.NewClient(consulClient.DefaultConfig())
	if err != nil {
		return nil, err
	}

	kv := consulCli.KV()

	p := &consulClient.KVPair{Key: AgentID + "/" + containerID, Value: []byte(strconv.Itoa(int(port)))}
	_, err = kv.Put(p, nil)
	if err != nil {
		return nil, err
	}
	return AgentGetStatus(ctx)
}
