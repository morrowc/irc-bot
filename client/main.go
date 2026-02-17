package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"golang.org/x/term"

	pbConfig "github.com/morrowc/irc-bot/proto/config"
	pbService "github.com/morrowc/irc-bot/proto/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/encoding/prototext"
)

// Global state
var (
	currentChannel string
	channels       []string
	msgHistory     = make(map[string][]*pbService.IRCMessage)
	mu             sync.RWMutex
	termState      *term.State
	stream         pbService.IRCService_StreamMessagesClient
)

func main() {
	// Connect to gRPC
	configPath := flag.String("config", "config.textproto", "Path to configuration file")
	flag.Parse()

	configFile, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("failed to read config file: %v", err)
	}

	config := &pbConfig.Config{}
	if err := prototext.Unmarshal(configFile, config); err != nil {
		log.Fatalf("failed to parse config file: %v", err)
	}

	tlsConfig := config.GetTls()
	if tlsConfig == nil {
		log.Fatal("TLS config missing in configuration file")
	}

	// Load CA
	caCert, err := os.ReadFile(tlsConfig.GetCaFile())
	if err != nil {
		log.Fatalf("failed to read CA cert: %v", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		log.Fatalf("failed to append CA cert")
	}

	// Load Client Cert/Key
	clientCert, err := tls.LoadX509KeyPair(tlsConfig.GetClientCertFile(), tlsConfig.GetClientKeyFile())
	if err != nil {
		log.Fatalf("failed to load client keypair: %v", err)
	}

	// Create TLS Config
	tConf := &tls.Config{
		RootCAs:      caCertPool,
		Certificates: []tls.Certificate{clientCert},
		ServerName:   "localhost", // Match server cert CN
	}
	creds := credentials.NewTLS(tConf)

	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pbService.NewIRCServiceClient(conn)

	// Subscribe
	ctx := context.Background()
	stream, err = client.StreamMessages(ctx)
	if err != nil {
		log.Fatalf("Error creating stream: %v", err)
	}

	// Send subscription
	if err := stream.Send(&pbService.StreamRequest{
		Request: &pbService.StreamRequest_Subscribe{
			Subscribe: &pbService.SubscribeRequest{
				GetHistory: true, // Request history
			},
		},
	}); err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}

	// Set raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Fatalf("Failed to set raw mode: %v", err)
	}
	termState = oldState
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Handle Input
	go handleInput()

	// Handle Output/Stream
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Failed to receive: %v", err)
		}

		switch e := in.Event.(type) {
		case *pbService.StreamEvent_Message:
			handleMessage(e.Message)
		case *pbService.StreamEvent_SystemMessage:
			handleSystemMessage(e.SystemMessage)
		}
	}
}

func handleInput() {
	reader := bufio.NewReader(os.Stdin)
	for {
		// Read byte by byte for control codes
		b, err := reader.ReadByte()
		if err != nil {
			return
		}

		switch b {
		case 3: // Ctrl-C
			// cleanup handled by defer in main? No, os.Exit wont run defers.
			// But signals will be caught if handled.
			// Raw mode interprets Ctrl-C as 3.
			term.Restore(int(os.Stdin.Fd()), termState)
			os.Exit(0)
		case 4: // Ctrl-D
			// Disconnect/Quit
			term.Restore(int(os.Stdin.Fd()), termState)
			os.Exit(0)
		case 14: // Ctrl-N (Next Channel)
			nextChannel()
		case 16: // Ctrl-P (Prev Channel)
			prevChannel()
		default:
			// Handle typing for sending messages (optional, not strictly in reqs but implied "client")
			// Req says "client should manage the terminal and accept simple control-character combinations... connect/disconnect... switch channels"
			// It doesn't explicitly say "send messages", but it's an IRC client.
			// "The example client should manage the terminal and accept simple control-character combinations to switch between channels... and a configurable command to disconnect"
			// It mentions "remote-client is connected messages for all connected channels should be transmitted...".
			// It doesn't explicitly mention sending.
			// I'll skip sending implementation for now to keep it simple, or just echo characters.
			// Printing characters in raw mode is manual.
			fmt.Print(string(b))
		}
	}
}

func handleMessage(msg *pbService.IRCMessage) {
	mu.Lock()
	defer mu.Unlock()

	ch := msg.GetChannel()
	msgHistory[ch] = append(msgHistory[ch], msg)

	// Add to channel list if new
	found := false
	for _, c := range channels {
		if c == ch {
			found = true
			break
		}
	}
	if !found {
		channels = append(channels, ch)
		if currentChannel == "" {
			currentChannel = ch
		}
	}

	if ch == currentChannel {
		// Print message
		// Needs proper cursor management in raw mode
		// \r\n for newline
		fmt.Printf("\r\n[%s] <%s> %s", msg.GetTimestamp().AsTime().Format("15:04"), msg.GetSender(), msg.GetContent())
	}
}

func handleSystemMessage(msg *pbService.SystemMessage) {
	fmt.Printf("\r\n[SYSTEM] %s", msg.GetContent())
}

func nextChannel() {
	mu.Lock()
	defer mu.Unlock()
	if len(channels) == 0 {
		return
	}

	for i, ch := range channels {
		if ch == currentChannel {
			next := (i + 1) % len(channels)
			currentChannel = channels[next]
			redraw()
			return
		}
	}
}

func prevChannel() {
	mu.Lock()
	defer mu.Unlock()
	if len(channels) == 0 {
		return
	}

	for i, ch := range channels {
		if ch == currentChannel {
			prev := (i - 1 + len(channels)) % len(channels)
			currentChannel = channels[prev]
			redraw()
			return
		}
	}
}

func redraw() {
	// Clear screen and redraw history of current channel
	fmt.Print("\033[H\033[2J") // ANSI clear
	fmt.Printf("Switched to %s\r\n", currentChannel)

	msgs := msgHistory[currentChannel]
	for _, msg := range msgs {
		fmt.Printf("[%s] <%s> %s\r\n", msg.GetTimestamp().AsTime().Format("15:04"), msg.GetSender(), msg.GetContent())
	}
}
