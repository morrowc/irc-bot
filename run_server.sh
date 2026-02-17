#!/bin/bash
# Ensure dependencies are tidy
go mod tidy

# Run the server
go run server/main.go server/irc_client.go server/grpc_server.go --config config.textproto
