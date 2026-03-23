#!/bin/bash
# Run the server using Bazel
bazel run //server:server -- --config $(pwd)/config.textproto
