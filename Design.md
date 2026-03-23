# IRC Bot / gRPC Bouncer Design Document

## Overview

The purpose of this project is to provide a split-architecture IRC client and bouncer system written in Go. It is designed to offer persistent IRC presence, secure remote access, and a clean, terminal-based user experience.

## Goals

### Server Architecture (Bouncer)

1. **Persistent Presence**: The server must maintain a continuous, persistent connection to the configured upstream IRC network (e.g., Libera.Chat), completely independent of client connectivity state.
2. **State Management & Buffering**: The server is responsible for tracking channel states and buffering recent messages for configured channels. When clients initially connect, they immediately receive historical context.
3. **gRPC API**: Communication exposed to clients must be implemented using gRPC, providing a robust, structured, and extensible streaming API for message delivery and command processing.
4. **Security**: All client-server communication must be strongly secured via Mutual TLS (mTLS). This ensures encryption in transit and strict cryptographic client authentication.

### Client Architecture

1. **Thin Client Design**: The client should act primarily as a user interface. Upstream connection lifecycle, IRC protocol parsing, and message storage are delegated entirely to the server.
2. **Terminal UI**: Provide a responsive terminal-based interface (using raw mode) for viewing messages, sending chat input, and easily navigating between active channels (e.g., `Ctrl-N` for next channel, `Ctrl-P` for previous channel).
3. **Seamless Attachment/Detachment**: Clients must be able to attach and detach seamlessly (e.g., via `Ctrl-D` or `Ctrl-C`) without interrupting or degrading the server's upstream IRC connection.

### Configuration

1. **Unified Configuration Management**: Both client and server rely on a structured, centralized `textproto` configuration format for managing IRC network details, server listener ports, channel history limits, and mTLS certificate paths.
