#!/bin/bash
# Ensure dependencies are tidy
go mod tidy

# Run the client
# go run client/main.go client/tui.go
# Note: tui.go doesn't exist separately, I put everything in main.go. 
# So just:
go run client/main.go
