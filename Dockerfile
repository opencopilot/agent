FROM golang:1.10

RUN apt-get update && apt-get -y install unzip && apt-get clean

# install protobuf
ENV PB_VER 3.5.1
ENV PB_URL https://github.com/google/protobuf/releases/download/v${PB_VER}/protoc-${PB_VER}-linux-x86_64.zip
RUN mkdir -p /tmp/protoc && \
    curl -L ${PB_URL} > /tmp/protoc/protoc.zip && \
    cd /tmp/protoc && \
    unzip protoc.zip && \
    cp /tmp/protoc/bin/protoc /usr/local/bin && \
    cp -R /tmp/protoc/include/* /usr/local/include && \
    chmod go+rx /usr/local/bin/protoc && \
    cd /tmp && \
    rm -r /tmp/protoc

RUN go get github.com/golang/protobuf/protoc-gen-go

WORKDIR /go/src/github.com/opencopilot/agent
COPY . .

# generate gRPC
RUN protoc -I ./manager ./manager/Manager.proto --go_out=plugins=grpc:./manager
RUN protoc -I ./agent ./agent/Agent.proto --go_out=plugins=grpc:./agent
RUN protoc -I ./health ./health/*.proto --go_out=plugins=grpc:./health

# https://github.com/moby/moby/issues/28269#issuecomment-382149133
# RUN go get github.com/docker/docker/client
# RUN rm -rf /go/src/github.com/docker/docker/vendor/github.com/docker/go-connections
# RUN go get github.com/docker/go-connections/nat
# RUN go get github.com/pkg/errors
# RUN go get -v -x
ENV DEP_VER 0.4.1
RUN curl -fsSL -o /usr/local/bin/dep https://github.com/golang/dep/releases/download/v${DEP_VER}/dep-linux-amd64 && chmod +x /usr/local/bin/dep
RUN dep ensure -vendor-only -v

RUN go build -o cmd/agent

EXPOSE 50051

ENTRYPOINT [ "cmd/agent" ]