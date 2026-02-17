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
	"strings"
	"sync"
	"time"

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

	// UI State
	width, height int
	inputBuffer   []rune
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

	// Pre-populate channels from config
	for _, ch := range config.GetChannels() {
		state.channels = append(state.channels, ch.GetName())
	}
	if len(state.channels) > 0 {
		state.currentChannel = state.channels[0]
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
		log.Fatalf("Failed to set raw mode: %n", err)
	}
	state.termState = oldState
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Initial draw
	state.updateSize()
	state.redraw()

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
		// Read rune (support unicode)
		r, _, err := reader.ReadRune()
		if err != nil {
			return
		}

		cs.mu.Lock()

		switch r {
		case 3: // Ctrl-C
			cs.mu.Unlock()
			if cs.termState != nil {
				term.Restore(int(os.Stdin.Fd()), cs.termState)
			}
			cs.exitFunc(0)
			return
		case 4: // Ctrl-D
			cs.mu.Unlock()
			if cs.termState != nil {
				term.Restore(int(os.Stdin.Fd()), cs.termState)
			}
			cs.exitFunc(0)
			return
		case 14: // Ctrl-N (Next Channel)
			cs.mu.Unlock()
			cs.nextChannel()
		case 16: // Ctrl-P (Prev Channel)
			cs.mu.Unlock()
			cs.prevChannel()
			if len(cs.inputBuffer) > 0 {
				msg := string(cs.inputBuffer)
				cs.inputBuffer = nil

				// Clear input line
				cs.moveToInput()

				if len(msg) > 0 && msg[0] == '/' {
					cs.handleCommand(msg)
				} else {
					// Send to Server
					if cs.stream != nil {
						// Start goroutine to send to avoid blocking input loop
						go func(ch, txt string) {
							err := cs.stream.Send(&pbService.StreamRequest{
								Request: &pbService.StreamRequest_SendMessage{
									SendMessage: &pbService.SendMessageRequest{
										Channel: ch,
										Message: txt,
									},
								},
							})
							if err != nil {
								// Log error?
							}
						}(cs.currentChannel, msg)
					}
				}
			}
			cs.mu.Unlock()
		case 127, 8: // Backspace
			if len(cs.inputBuffer) > 0 {
				cs.inputBuffer = cs.inputBuffer[:len(cs.inputBuffer)-1]
				cs.moveToInput()
			}
			cs.mu.Unlock()
		default:
			// Filter control chars
			if r >= 32 {
				cs.inputBuffer = append(cs.inputBuffer, r)
				cs.moveToInput()
			}
			cs.mu.Unlock()
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
		// Save Cursor
		fmt.Fprint(cs.out, "\0337")

		// Move to bottom of scroll region
		fmt.Fprintf(cs.out, "\033[%d;1H", cs.height-2)
		fmt.Fprintf(cs.out, "\r\n[%s] <%s> %s", msg.GetTimestamp().AsTime().Format("15:04"), msg.GetSender(), msg.GetContent())

		// Restore Cursor
		fmt.Fprint(cs.out, "\0338")
		// Or just force redraw input?
		// If we printed \n at bottom of scroll region, it scrolled up.
		// Status bar and Input line should be unaffected if outside scroll region.
		// But let's be safe and redraw status if needed?
		// Actually simple appending might work if scroll region is set correctly.
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

func (cs *ClientState) updateSize() {
	w, h, err := term.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		// Fallback?
		return
	}
	cs.width = w
	cs.height = h
	cs.setScrollRegion(1, h-2)
}

func (cs *ClientState) setScrollRegion(top, bot int) {
	fmt.Fprintf(cs.out, "\033[%d;%dr", top, bot)
}

func (cs *ClientState) drawStatusBar() {
	// Move to Status Line (height-1)
	fmt.Fprintf(cs.out, "\033[%d;1H", cs.height-1) // Move cursor
	fmt.Fprintf(cs.out, "\033[7m")                 // Invert colors

	status := fmt.Sprintf("[ Channel: %s ]", cs.currentChannel)
	// Pad with spaces to width
	for len(status) < cs.width {
		status += " "
	}
	fmt.Fprint(cs.out, status)
	fmt.Fprintf(cs.out, "\033[0m") // Reset colors
}

func (cs *ClientState) moveToInput() {
	fmt.Fprintf(cs.out, "\033[%d;1H", cs.height)
	// Reprint buffer
	fmt.Fprint(cs.out, "\033[2K") // Clear line
	fmt.Fprintf(cs.out, "> %s", string(cs.inputBuffer))
}

func (cs *ClientState) redraw() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Clear screen
	fmt.Fprint(cs.out, "\033[H\033[2J")

	cs.updateSize() // Ensure size is current
	cs.drawStatusBar()

	// Draw correct number of past messages in the scroll region
	// The scroll region is 1 to height-2
	// We should print the last (height-2) messages
	msgs := cs.msgHistory[cs.currentChannel]
	maxMsgs := cs.height - 2
	start := 0
	if len(msgs) > maxMsgs {
		start = len(msgs) - maxMsgs
	}

	// Move to top
	fmt.Fprint(cs.out, "\033[1;1H")
	for i := start; i < len(msgs); i++ {
		msg := msgs[i]
		fmt.Fprintf(cs.out, "[%s] <%s> %s\r\n", msg.GetTimestamp().AsTime().Format("15:04"), msg.GetSender(), msg.GetContent())
	}

	cs.moveToInput()
}

func (cs *ClientState) handleCommand(cmd string) {
	// Basic parsing
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "/disconnect":
		if cs.termState != nil {
			term.Restore(int(os.Stdin.Fd()), cs.termState)
		}
		cs.exitFunc(0)
	case "/history":
		// Request history for current channel
		if cs.stream != nil {
			go func() {
				err := cs.stream.Send(&pbService.StreamRequest{
					Request: &pbService.StreamRequest_Subscribe{
						Subscribe: &pbService.SubscribeRequest{
							GetHistory: true,
						},
					},
				})
				if err != nil {
					// log
				}
			}()
		}
	case "/quit":
		// Shutdown server
		// Usage: /quit <password>
		if len(parts) < 2 {
			// Print error to local output?
			// Need a way to print local system message
			return
		}
		password := parts[1]
		if cs.stream != nil {
			go func() {
				err := cs.stream.Send(&pbService.StreamRequest{
					Request: &pbService.StreamRequest_Quit{
						Quit: &pbService.QuitRequest{
							ShutdownServer: true,
							Password:       password,
						},
					},
				})
				if err != nil {
					// log
				}
			}()
			// Also disconnect client?
			// Maybe wait for server to close stream?
			// But server might die immediately.
			time.Sleep(500 * time.Millisecond)
			if cs.termState != nil {
				term.Restore(int(os.Stdin.Fd()), cs.termState)
			}
			cs.exitFunc(0)
		}
	}
}
