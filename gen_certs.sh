#!/bin/bash
set -e

# Directory for certs
mkdir -p certs
cd certs

# 1. Generate CA
echo "Generating CA..."
openssl genrsa -out ca.key 4096
openssl req -new -x509 -key ca.key -sha256 -subj "/C=US/ST=State/L=City/O=IRC-Bot-CA/CN=IRC-Bot-Root-CA" -days 3650 -out ca.crt

# 2. Generate Server Cert
echo "Generating Server Cert..."
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr -subj "/C=US/ST=State/L=City/O=IRC-Bot-Server/CN=localhost"
# Sign Server Cert
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365 -sha256 -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1")

# 3. Generate Client Cert
CLIENT_CN="${1:-client_user}"
echo "Generating Client Cert for CN=${CLIENT_CN}..."
openssl genrsa -out client.key 4096
openssl req -new -key client.key -out client.csr -subj "/C=US/ST=State/L=City/O=IRC-Bot-Client/CN=${CLIENT_CN}"
# Sign Client Cert
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt -days 365 -sha256

echo "Certificates generated in certs/"
echo "CA: ca.crt"
echo "Server: server.crt, server.key"
echo "Client: client.crt, client.key"
