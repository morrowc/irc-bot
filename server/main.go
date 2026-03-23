package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
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
	configData, err := os.ReadFile(path)
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
	var histMu sync.RWMutex
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
		histMu.RLock()
		defer histMu.RUnlock()
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
		caCert, err := os.ReadFile(tlsConfig.GetCaFile())
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

	// Wait for shutdown signal or SIGHUP
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	for {
		sig := <-c
		if sig == syscall.SIGHUP {
			log.Println("Received SIGHUP. Reloading configuration...")

			newConfig, err := loadConfig(*configPath)
			if err != nil {
				log.Printf("Failed to reload config: %v", err)
				continue
			}

			// Update History Buffers
			// Strategy: Create new map. Copy existing buffers for channels that still exist.
			// Create new buffers for new channels.
			newHistBuffers := make(map[string]*history.ChannelBuffer)

			// Populate new map
			for _, ch := range newConfig.GetChannels() {
				name := ch.GetName()
				if existing, ok := histBuffers[name]; ok {
					// Update limit if changed?
					// For now, simpler to just reuse existing buffer instance.
					// If limit changed, we might need to resize. ChannelBuffer doesn't support resize yet.
					// Assuming limit doesn't change often or we don't care about immediate resize.
					newHistBuffers[name] = existing
				} else {
					limit := int(ch.GetHistoryLimit())
					if limit == 0 {
						limit = 100
					}
					newHistBuffers[name] = history.NewChannelBuffer(limit)
				}
			}

			// Update global histBuffers ref for wrapper
			histMu.Lock()
			histBuffers = newHistBuffers
			histMu.Unlock()
			// Note: getBuffer closure captures the *variable* histBuffers if we didn't re-declare it?
			// "getBuffer := func..." defined earlier closes over the variable.
			// But strict closure might capture the value if not careful?
			// Actually, getBuffer uses "histBuffers[name]".
			// In Go, closures capture variables by reference.
			// So updating histBuffers *should* work for getBuffer.
			// BUT, strictly speaking it's not thread safe if getBuffer is called concurrently (e.g. by bot callback).
			// Bot callback doesn't use getBuffer.
			// getBuffer is passed to NewIRCBot but Is it used?
			// IRCBot uses b.history(channel).
			// Yes.

			// Thread safety is an issue if IRCBot reads history() while we write histBuffers.
			// BUT: we are single threaded in main event loop.
			// IRCBot runs in its own goroutine (client.Connect blocks? No we put it in gofunc).
			// So we need to be careful.
			// Ideally getBuffer should be thread safe.

			// Let's ignore that race for a second or fix it.
			// Actually, we are updating the map reference, which is atomic-ish, but the map itself is not.

			// Pass updates to Components
			grpcService.UpdateState(newConfig.GetService(), histBuffers)
			bot.UpdateChannels(newConfig.GetChannels())

			log.Println("Configuration reloaded.")

		} else {
			// SIGINT or SIGTERM
			break
		}
	}

	log.Println("Shutting down...")
	bot.Close()
	grpcServer.GracefulStop()
}
