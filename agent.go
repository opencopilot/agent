package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"github.com/docker/docker/api/types/filters"

	"github.com/buger/jsonparser"
	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockerClient "github.com/docker/docker/client"
	consul "github.com/hashicorp/consul/api"
	pb "github.com/opencopilot/agent/agent"
	consulkvjson "github.com/opencopilot/consul-kv-json"
)

// Service is a specification for a service running on the device
type Service string

// Services is a list of Service
type Services []Service

// AgentGetStatus returns the status of a running service
func AgentGetStatus(ctx context.Context) (*pb.AgentStatus, error) {
	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		return nil, err
	}
	status := &pb.AgentStatus{InstanceId: InstanceID, Services: []*pb.AgentStatus_AgentService{}}

	containers, err := dockerCli.ContainerList(ctx, dockerTypes.ContainerListOptions{})
	if err != nil {
		log.Fatal(err)
	}

	for _, container := range containers {
		status.Services = append(status.Services, &pb.AgentStatus_AgentService{Id: container.ID, Image: container.Image})
	}

	return status, nil
}

// ConfigHandler runs when the service configuration for this instance changes
func ConfigHandler(kvs consul.KVPairs) {
	m, err := consulkvjson.ConsulKVsToJSON(kvs)
	if err != nil {
		log.Panic(err)
	}
	jsonString, err := json.Marshal(m)
	if err != nil {
		log.Panic(err)
	}
	// fmt.Printf("%s, %v", jsonString, m)
	_, valueType, _, err := jsonparser.Get(jsonString, "instances", InstanceID, "services")
	if valueType == jsonparser.NotExist {
		ensureServices(Services{})
		return
	}

	if err != nil {
		log.Fatal(err)
	}

	type mt = map[string]interface{}
	servicesMap := m["instances"].(mt)[InstanceID].(mt)["services"].(mt)
	incomingServices := Services{}
	for s := range servicesMap {
		service := Service(s)
		incomingServices = append(incomingServices, service)
	}

	ensureServices(incomingServices)
}

func getLocalServices() (Services, error) {
	cli, err := dockerClient.NewEnvClient()
	if err != nil {
		return nil, err
	}

	args := filters.NewArgs(
		filters.Arg("label", "com.opencopilot.managed"),
	)
	containers, err := cli.ContainerList(context.Background(), dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		log.Panic(err)
	}

	localServices := Services{}
	for _, container := range containers {
		serviceName, found := container.Labels["com.opencopilot.service"]
		if !found {
			continue
		}
		// fmt.Printf("%s %s %v\n", container.ID[:10], container.Image, serviceName)
		service := Service(serviceName)
		localServices = append(localServices, service)
	}
	return localServices, nil
}

func ensureServices(incomingServices Services) {
	localServices, err := getLocalServices()
	if err != nil {
		log.Panicln(err)
	}

	for _, incomingService := range incomingServices {
		// For every service we should have, go check all the services we're currently running
		existsLocally := false
		for _, localService := range localServices {
			// If an incoming service is already running, do nothing
			if incomingService == localService {
				existsLocally = true
			}
		}

		// If we didn't find that this incoming service exists locally, start it
		if existsLocally {
			break
		} else {
			err := startService(incomingService)
			if err != nil {
				// TODO: do something else here
				log.Println(err)
			}
		}
	}

	for _, localService := range localServices {
		// For every locally running service, check if it should be running from the incoming services
		existsIncoming := false
		for _, incomingService := range incomingServices {
			if localService == incomingService {
				existsIncoming = true
			}
		}

		// If we didn't find that this local service exists in the incoming specification, stop it
		if existsIncoming {
			break
		} else {
			err := stopService(localService)
			if err != nil {
				// TODO: do something else here
				log.Println(err)
			}
		}

	}
}

func startService(service Service) error {
	log.Printf("Adding service: %s\n", string(service))

	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		return err
	}

	ctx := context.Background()

	var containerConfig *container.Config
	switch string(service) {
	case "LB":
		containerConfig = &container.Config{
			Image: "quay.io/opencopilot/haproxy-manager",
			Labels: map[string]string{
				"com.opencopilot.managed": "",
				"com.opencopilot.service": string(service),
			},
		}
	default:
		return errors.New("Invalid service specified")
	}

	reader, err := dockerCli.ImagePull(ctx, containerConfig.Image, dockerTypes.ImagePullOptions{})
	if err != nil {
		return err
	}
	reader.Close()

	res, err := dockerCli.ContainerCreate(ctx, containerConfig, &container.HostConfig{
		AutoRemove: true, // Important to remove container after it's stopped, so that we can start a new one up with the same name if this service gets re-added
	}, nil, "com.opencopilot.service."+string(service))
	if err != nil {
		return err
	}

	err2 := dockerCli.ContainerStart(ctx, res.ID, dockerTypes.ContainerStartOptions{})
	if err2 != nil {
		return err
	}

	return nil
}

func stopService(service Service) error {
	log.Printf("Stopping service: %s\n", string(service))
	dockerCli, err := dockerClient.NewEnvClient()
	if err != nil {
		return err
	}

	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("label", "com.opencopilot.managed"),
		filters.Arg("name", "com.opencopilot.service."+string(service)),
	)
	containers, err := dockerCli.ContainerList(context.Background(), dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, container := range containers {
		dockerCli.ContainerStop(ctx, container.ID, nil)
	}

	return nil
}

func configureService() {

}
