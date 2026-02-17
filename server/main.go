package main

import (
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
	"google.golang.org/protobuf/encoding/prototext"
)

var (
	configPath = flag.String("config", "config.textproto", "Path to configuration file")
)

func main() {
	flag.Parse()

	// Load Configuration
	configData, err := ioutil.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	config := &pbConfig.Config{}
	if err := prototext.Unmarshal(configData, config); err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
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
    lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.GetService().GetPort()))
    if err != nil {
        log.Fatalf("failed to listen: %v", err)
    }
    
    grpcServer := grpc.NewServer()
    // TODO: Add TLS credentials if configured
    
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
