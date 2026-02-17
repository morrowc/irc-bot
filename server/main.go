package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	pbConfig "github.com/morrowc/irc-bot/proto/config"
	pbService "github.com/morrowc/irc-bot/proto/service"
	"github.com/morrowc/irc-bot/server/history"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/encoding/prototext"
)

var (
	configPath = flag.String("config", "config.textproto", "Path to configuration file")
)

func loadConfig(path string) (*pbConfig.Config, error) {
	// Load Configuration
	configData, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	config := &pbConfig.Config{}
	if err := prototext.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	return config, nil
}

func main() {
	flag.Parse()

	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	// Initialize History Buffers
	histBuffers := make(map[string]*history.ChannelBuffer)
	for _, ch := range config.GetChannels() {
		limit := int(ch.GetHistoryLimit())
		if limit == 0 {
			limit = 100 // Default if 0? Or just 0.
		}
		histBuffers[ch.GetName()] = history.NewChannelBuffer(limit)
	}

	// Helper to get buffer safely
	getBuffer := func(name string) *history.ChannelBuffer {
		return histBuffers[name]
	}
	// Initialize gRPC Service
	grpcService := NewIRCServiceServer(config.GetService(), histBuffers)
	
	// Helper to broadcast to gRPC clients
	broadcaster := func(msg *pbService.IRCMessage) {
		grpcService.Broadcast(msg)
	}

	// Start IRC Client
	bot := NewIRCBot(config.GetIrc(), config.GetChannels(), getBuffer, broadcaster)

    // Link bot to service
	grpcService.SetBot(bot)

	go func() {
		if err := bot.Connect(); err != nil {
			log.Fatalf("IRC Connect failed: %v", err)
		}
	}()

	// Join channels on connect (handled in IRCBot or manually here?)
	// girc has auto-join if configured, but let's do it manually or via callback.
	// simpler: bot.Join(...) called after connect?
	// Actually girc connect blocks. So we should configure it to auto-join or handle 001 event.
	// Let's rely on the bot to handle rejoins if possible, or adds a handler for 001.

	// Start gRPC Server
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", config.GetService().GetHost(), config.GetService().GetPort()))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// mTLS Configuration
	tlsConfig := config.GetTls()
	var opts []grpc.ServerOption

	if tlsConfig != nil {
		// Load CA
		caCert, err := ioutil.ReadFile(tlsConfig.GetCaFile())
		if err != nil {
			log.Fatalf("failed to read CA cert: %v", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			log.Fatalf("failed to append CA cert")
		}

		// Load Server Cert/Key
		serverCert, err := tls.LoadX509KeyPair(tlsConfig.GetCertFile(), tlsConfig.GetKeyFile())
		if err != nil {
			log.Fatalf("failed to load server keypair: %v", err)
		}

		// Create TLS Config
		tConf := &tls.Config{
			ClientCAs:    caCertPool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
			Certificates: []tls.Certificate{serverCert},
			VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
				// Check Client CN
				// Note: verifiedChains[0][0] is the leaf certificate
				if len(verifiedChains) > 0 && len(verifiedChains[0]) > 0 {
					clientCert := verifiedChains[0][0]
					expectedCN := tlsConfig.GetClientCn()
					if clientCert.Subject.CommonName != expectedCN {
						return fmt.Errorf("client CN %q does not match expected %q", clientCert.Subject.CommonName, expectedCN)
					}
				}
				return nil
			},
		}
		creds := credentials.NewTLS(tConf)
		opts = append(opts, grpc.Creds(creds))
	}

	grpcServer := grpc.NewServer(opts...)

	pbService.RegisterIRCServiceServer(grpcServer, grpcService)

	go func() {
		log.Printf("Starting gRPC server on :%d", config.GetService().GetPort())
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// Wait for shutdown signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("Shutting down...")
	bot.Close()
	grpcServer.GracefulStop()
}
