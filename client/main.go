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

// ClientState manages the client logic and state
type ClientState struct {
	currentChannel string
	channels       []string
	msgHistory     map[string][]*pbService.IRCMessage
	mu             sync.RWMutex
	termState      *term.State
	stream         pbService.IRCService_StreamMessagesClient
	out            io.Writer // For testing output
	exitFunc       func(int) // For testing exit
}

func NewClientState() *ClientState {
	return &ClientState{
		msgHistory: make(map[string][]*pbService.IRCMessage),
		out:        os.Stdout,
		exitFunc:   os.Exit,
	}
}

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
	stream, err := client.StreamMessages(ctx)
	if err != nil {
		log.Fatalf("Error creating stream: %v", err)
	}

	// Initialize State
	state := NewClientState()
	state.stream = stream

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
		log.Fatalf("Failed to set raw mode: %n", err)
	}
	state.termState = oldState
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Handle Input
	go state.handleInput(os.Stdin)

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
			state.handleMessage(e.Message)
		case *pbService.StreamEvent_SystemMessage:
			state.handleSystemMessage(e.SystemMessage)
		}
	}
}

func (cs *ClientState) handleInput(input io.Reader) {
	reader := bufio.NewReader(input)
	for {
		// Read byte by byte for control codes
		b, err := reader.ReadByte()
		if err != nil {
			return
		}

		switch b {
		case 3: // Ctrl-C
			if cs.termState != nil {
				term.Restore(int(os.Stdin.Fd()), cs.termState)
			}
			cs.exitFunc(0)
		case 4: // Ctrl-D
			if cs.termState != nil {
				term.Restore(int(os.Stdin.Fd()), cs.termState)
			}
			cs.exitFunc(0)
		case 14: // Ctrl-N (Next Channel)
			cs.nextChannel()
		case 16: // Ctrl-P (Prev Channel)
			cs.prevChannel()
		default:
			fmt.Fprint(cs.out, string(b))
		}
	}
}

func (cs *ClientState) handleMessage(msg *pbService.IRCMessage) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	ch := msg.GetChannel()
	cs.msgHistory[ch] = append(cs.msgHistory[ch], msg)

	// Add to channel list if new
	found := false
	for _, c := range cs.channels {
		if c == ch {
			found = true
			break
		}
	}
	if !found {
		cs.channels = append(cs.channels, ch)
		if cs.currentChannel == "" {
			cs.currentChannel = ch
		}
	}

	if ch == cs.currentChannel {
		fmt.Fprintf(cs.out, "\r\n[%s] <%s> %s", msg.GetTimestamp().AsTime().Format("15:04"), msg.GetSender(), msg.GetContent())
	}
}

func (cs *ClientState) handleSystemMessage(msg *pbService.SystemMessage) {
	fmt.Fprintf(cs.out, "\r\n[SYSTEM] %s", msg.GetContent())
}

func (cs *ClientState) nextChannel() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.channels) == 0 {
		return
	}

	for i, ch := range cs.channels {
		if ch == cs.currentChannel {
			next := (i + 1) % len(cs.channels)
			cs.currentChannel = cs.channels[next]
			cs.redraw()
			return
		}
	}
}

func (cs *ClientState) prevChannel() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.channels) == 0 {
		return
	}

	for i, ch := range cs.channels {
		if ch == cs.currentChannel {
			prev := (i - 1 + len(cs.channels)) % len(cs.channels)
			cs.currentChannel = cs.channels[prev]
			cs.redraw()
			return
		}
	}
}

func (cs *ClientState) redraw() {
	// Clear screen and redraw history of current channel
	fmt.Fprint(cs.out, "\033[H\033[2J") // ANSI clear
	fmt.Fprintf(cs.out, "Switched to %s\r\n", cs.currentChannel)

	msgs := cs.msgHistory[cs.currentChannel]
	for _, msg := range msgs {
		fmt.Fprintf(cs.out, "[%s] <%s> %s\r\n", msg.GetTimestamp().AsTime().Format("15:04"), msg.GetSender(), msg.GetContent())
	}
}
