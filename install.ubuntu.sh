#!/bin/sh

#### Install Docker ###
curl -fsSL get.docker.com -o get-docker.sh
sh get-docker.sh

### Install Consul ###
apt-get install unzip
wget https://releases.hashicorp.com/consul/1.0.6/consul_1.0.6_linux_amd64.zip
unzip consul_1.0.6_linux_amd64.zip
mv consul /usr/bin/consul

### Setup Consul ###
# TODO: setup consul to join the right cluster as a client
echo "
[Unit]
Description=Consul agent as client
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root
ExecStart=/usr/bin/consul agent -dev
Restart=on-failure

[Install]
WantedBy=multi-user.target
" >> /etc/systemd/system/consul.service

### Start Consul ###
systemctl start consul

### Install ocp-agent ###

# TODO: pull from github?

### Setup ocp-agent ###

# TODO: similar to Consul setup, add systemd service
# TODO: will likely have to grab keys from somewhere...Vault? for encrypting public gRPC endpoint

### Start ocp-agent ###

# TODO: systemctl start ocp-agent