package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"strconv"

	"github.com/docker/docker/api/types/filters"
	"google.golang.org/grpc"

	"github.com/buger/jsonparser"
	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	docker "github.com/docker/docker/client"
	consul "github.com/hashicorp/consul/api"
	pb "github.com/opencopilot/agent/agent"
	managerPb "github.com/opencopilot/agent/manager"
	"github.com/opencopilot/consul-kv-json"
	"gopkg.in/yaml.v2"
)

// Service is a specification for a service running on the device
type Service string

// Services is a list of Service
type Services []Service

// Agent handles agent functionality
type Agent struct {
	dockerCli *docker.Client
	consulCli *consul.Client
}

// AgentGetStatus returns the status of a running service
func (agent *Agent) AgentGetStatus(ctx context.Context) (*pb.AgentStatus, error) {

	status := &pb.AgentStatus{InstanceId: InstanceID, Services: []*pb.AgentStatus_AgentService{}}

	containers, err := agent.dockerCli.ContainerList(ctx, dockerTypes.ContainerListOptions{})
	if err != nil {
		log.Fatal(err)
	}

	for _, container := range containers {
		status.Services = append(status.Services, &pb.AgentStatus_AgentService{Id: container.ID, Image: container.Image})
	}

	return status, nil
}

func (agent *Agent) startConfigHandler(queue chan consul.KVPairs) {
	for {
		kvs := <-queue
		agent.sync(kvs)
	}
}

func (agent *Agent) sync(kvs consul.KVPairs) {
	m, err := consulkvjson.ConsulKVsToJSON(kvs)
	if err != nil {
		log.Panic(err)
	}
	jsonString, err := json.Marshal(m)
	if err != nil {
		log.Panic(err)
	}

	_, valueType, _, err := jsonparser.Get(jsonString, "instances", InstanceID, "services")
	if valueType == jsonparser.NotExist {
		agent.ensureServices(Services{})
		return
	}

	if err != nil {
		log.Fatal(err)
	}

	type mt = map[string]interface{}
	servicesMap := m["instances"].(mt)[InstanceID].(mt)["services"].(mt) // TODO use that json parsing library
	incomingServices := Services{}
	for s := range servicesMap {
		service := Service(s)
		incomingServices = append(incomingServices, service)
	}

	agent.ensureServices(incomingServices)
	localServices, err := agent.getLocalServices()
	if err != nil {
		log.Fatal(err)
	}
	agent.configureServices(localServices)
}

func (agent *Agent) getLocalServices() (Services, error) {
	args := filters.NewArgs(
		filters.Arg("label", "com.opencopilot.managed"),
	)
	containers, err := agent.dockerCli.ContainerList(context.Background(), dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		log.Panic(err)
	}

	localServices := Services{}
	for _, container := range containers {
		serviceName, found := container.Labels["com.opencopilot.service-manager"]
		if !found {
			continue
		}
		// fmt.Printf("%s %s %v\n", container.ID[:10], container.Image, serviceName)
		service := Service(serviceName)
		localServices = append(localServices, service)
	}
	return localServices, nil
}

func (agent *Agent) ensureServices(incomingServices Services) {
	localServices, err := agent.getLocalServices()
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
			err := agent.startService(incomingService)
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
			err := agent.stopService(localService)
			if err != nil {
				// TODO: do something else here
				log.Println(err)
			}
		}

	}
}

func (agent *Agent) startService(service Service) error {
	log.Printf("adding service: %s\n", string(service))

	ctx := context.Background()
	serviceImageYaml, err := ioutil.ReadFile("./services.yaml")
	if err != nil {
		log.Fatal(err)
	}
	serviceImageMap := make(map[interface{}]interface{})
	yamlErr := yaml.Unmarshal(serviceImageYaml, &serviceImageMap)
	if yamlErr != nil {
		log.Fatal(yamlErr)
	}

	if serviceImageMap[string(service)] == nil {
		log.Fatal(errors.New("invalid service specified"))
	}

	containerConfig := &container.Config{
		Labels: map[string]string{
			"com.opencopilot.managed":         "",
			"com.opencopilot.service-manager": string(service),
		},
		Image: serviceImageMap[string(service)].(string),
	}

	reader, err := agent.dockerCli.ImagePull(ctx, containerConfig.Image, dockerTypes.ImagePullOptions{})
	if err != nil {
		return err
	}

	defer reader.Close()
	if _, err := ioutil.ReadAll(reader); err != nil {
		log.Panic(err)
	}

	containerConfig.Env = []string{"CONFIG_DIR=" + ConfigDir, "INSTANCE_ID=" + InstanceID}

	res, err := agent.dockerCli.ContainerCreate(ctx, containerConfig, &container.HostConfig{
		AutoRemove: true, // Important to remove container after it's stopped, so that we can start a new one up with the same name if this service gets re-added
		Privileged: true, // So that the manager containers can start other docker containers,
		Binds: []string{ // So that the manager containers have access to Docker on the host
			"/var/run/docker.sock:/var/run/docker.sock",
			ConfigDir + ":" + ConfigDir,
		},
		PublishAllPorts: true,
	}, nil, "com.opencopilot.service-manager."+string(service))
	if err != nil {
		return err
	}

	startErr := agent.dockerCli.ContainerStart(ctx, res.ID, dockerTypes.ContainerStartOptions{})
	if startErr != nil {
		return startErr
	}

	return nil
}

func (agent *Agent) stopService(service Service) error {
	log.Printf("stopping service: %s\n", string(service))

	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("label", "com.opencopilot.managed"),
		filters.Arg("name", "com.opencopilot.service-manager."+string(service)),
	)
	containers, err := agent.dockerCli.ContainerList(ctx, dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, container := range containers {
		agent.dockerCli.ContainerStop(ctx, container.ID, nil)
	}

	return nil
}

func (agent *Agent) configureService(service Service) error {
	ctx := context.Background()
	args := filters.NewArgs(
		filters.Arg("label", "com.opencopilot.managed"),
		filters.Arg("name", "com.opencopilot.service-manager."+string(service)),
	)
	containers, err := agent.dockerCli.ContainerList(ctx, dockerTypes.ContainerListOptions{
		Filters: args,
	})
	if err != nil {
		log.Fatal(err)
	}

	kv := agent.consulCli.KV()

	for _, container := range containers {
		// log.Println(container.Ports)
		var gRPCPort uint16
		for _, portPair := range container.Ports {
			if portPair.PrivatePort == 50052 {
				gRPCPort = portPair.PublicPort
			}
		}

		kvs, _, err := kv.List("instances/"+InstanceID+"/services/"+string(service), &consul.QueryOptions{})
		if err != nil {
			log.Fatal(err)
		}
		configMap, err := consulkvjson.ConsulKVsToJSON(kvs)
		if err != nil {
			log.Fatal(err)
		}

		configString, err := json.Marshal(configMap)
		if err != nil {
			log.Fatal(err)
		}

		serviceConfig, dataType, _, err := jsonparser.Get(configString, "instances", InstanceID, "services", string(service))
		if err != nil {
			log.Fatal(err)
		}
		if dataType == jsonparser.NotExist {
			log.Println(errors.New("invalid JSON"))
		}

		conn, err := grpc.Dial("localhost:"+strconv.Itoa(int(gRPCPort)), grpc.WithInsecure())
		if err != nil {
			log.Fatalf("fail to dial: %v", err)
		}
		defer conn.Close()

		client := managerPb.NewManagerClient(conn)
		_, errConfiguring := client.Configure(ctx, &managerPb.ConfigureRequest{Config: string(serviceConfig)})
		if errConfiguring != nil {
			return errConfiguring
		}

	}
	return nil
}

func (agent *Agent) configureServices(services Services) []error {
	var errorList []error
	for _, service := range services {
		err := agent.configureService(service)
		if err != nil {
			errorList = append(errorList, err)
		}
	}
	return errorList
}
