### ocp-agent

This is the ocp-agent, a lightweight daemon that runs on a device managed by OpenCoPilot and exposes a gRPC endpoint for ocp-core to communicate with. It's job is to forward and translate gRPC calls from ocp-core to services running on the same ocp-managed device.