### ocopi-agent

This is the ocopi-agent, a lightweight daemon that runs on a device managed by OpenCoPilot and exposes a gRPC endpoint for ocopi-core to communicate with. It's job is to forward and translate gRPC calls from ocopi-core to services running on the same ocopi-managed device.