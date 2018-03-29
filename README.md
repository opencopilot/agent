### plumb-agent

This is the plumb-agent, a lightweight daemon that runs on a device managed by plumb and exposes a gRPC endpoint for plumb-core to communicate with. It's job is to forward and translate gRPC calls from plumb-core to services running on the same plumb-managed device.