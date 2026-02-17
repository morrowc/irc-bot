# Go IRC Bot / gRPC Bouncer

This repository contains a split-architecture IRC client/bouncer written in Go.

## Architecture

The system consists of two main components:

1. **Server (`server/`)**:
    * Connects to an upstream IRC network (e.g., Libera.Chat).
    * Maintains a persistent connection and buffers recent messages for configured channels.
    * Exposes a gRPC service for clients to connect, retrieve history, and receive live updates.
    * Supports mTLS (Mutual TLS) for secure client-server communication.

2. **Client (`client/`)**:
    * Connects to the Server via gRPC using mTLS.
    * Provides a terminal interface (raw mode) to view messages.
    * Supports channel switching (`Ctrl-N`, `Ctrl-P`) and graceful disconnect (`Ctrl-D`).

## Features

* **Persistent Presence**: The server stays connected even when the client disconnects.
* **Message History**: Clients receive recent message history upon connection.
* **Security**: gRPC connection is secured with Mutual TLS (mTLS), ensuring only authorized clients can connect.
* **Configuration**: All configuration is handled via a `textproto` file for readability.

## Setup & Usage

### 1. Generate Certificates

Run the helper script to generate a Certificate Authority (CA), Server, and Client certificates:

```bash
./gen_certs.sh
```

This will create a `certs/` directory containing:

* `ca.crt`, `ca.key`
* `server.crt`, `server.key`
* `client.crt`, `client.key` (Common Name: `client_user`)

### 2. Configure

Edit `config.textproto` to set your IRC details and certificate paths:

```textproto
irc: {
  host: "irc.libera.chat"
  port: 6697
  use_tls: true
  nick: "MyBotNick"
  user: "MyBotUser"
  # password: "optional_password"
}
channels: {
  name: "#go-nuts"
  history_limit: 100
}
service: {
  port: 50051
}
tls: {
  ca_file: "certs/ca.crt"
  cert_file: "certs/server.crt"
  key_file: "certs/server.key"
  client_cn: "client_user" # Must match CN in gen_certs.sh
  client_cert_file: "certs/client.crt"
  client_key_file: "certs/client.key"
}
```

### 3. Run Server

```bash
go run server/*.go -config config.textproto
```

### 4. Run Client

```bash
go run client/*.go -config config.textproto
```

## Controls (Client)

* **Ctrl-N**: Next Channel
* **Ctrl-P**: Previous Channel
* **Ctrl-C / Ctrl-D**: Quit

## Testing

Run unit tests for all components:

```bash
go test -v ./...
```
