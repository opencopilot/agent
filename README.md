### OpenCoPilot Agent

This is daemon that runs on a device managed by OpenCoPilot and exposes a gRPC endpoint for core to communicate with. It's job is to forward and translate gRPC calls from core to services running on the same managed device.