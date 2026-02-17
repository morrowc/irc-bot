package main

import (
	"context"
	"io"
	"testing"
	"time"

	pbConfig "github.com/morrowc/irc-bot/proto/config"
	pbService "github.com/morrowc/irc-bot/proto/service"
	"github.com/morrowc/irc-bot/server/history"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MockStream implements pbService.IRCService_StreamMessagesServer
type MockStream struct {
	grpc.ServerStream
	ctx       context.Context
	recvChan  chan *pbService.StreamRequest
	sentMsgs  []*pbService.StreamEvent
	closeChan chan struct{}
}

func NewMockStream(ctx context.Context) *MockStream {
	return &MockStream{
		ctx:       ctx,
		recvChan:  make(chan *pbService.StreamRequest, 10),
		sentMsgs:  make([]*pbService.StreamEvent, 0),
		closeChan: make(chan struct{}),
	}
}

func (m *MockStream) Context() context.Context {
	return m.ctx
}

func (m *MockStream) Send(msg *pbService.StreamEvent) error {
	m.sentMsgs = append(m.sentMsgs, msg)
	return nil
}

func (m *MockStream) Recv() (*pbService.StreamRequest, error) {
	select {
	case msg := <-m.recvChan:
		return msg, nil
	case <-m.closeChan:
		return nil, io.EOF
	case <-m.ctx.Done():
		return nil, m.ctx.Err()
	}
}

func TestStreamMessages_History(t *testing.T) {
	// Setup
	hist := make(map[string]*history.ChannelBuffer)
	cb := history.NewChannelBuffer(10)
	cb.Add(&pbService.IRCMessage{
		Content:   "historical_msg",
		Timestamp: timestamppb.Now(),
		Channel:   "#test",
	})
	hist["#test"] = cb

	cfg := &pbConfig.Service{Port: 1234}
	srv := NewIRCServiceServer(cfg, hist)

	// Mock Stream
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := NewMockStream(ctx)

	// Inject Subscribe Request
	stream.recvChan <- &pbService.StreamRequest{
		Request: &pbService.StreamRequest_Subscribe{
			Subscribe: &pbService.SubscribeRequest{
				GetHistory: true,
			},
		},
	}

	// Run StreamMessages in goroutine as it blocks
	errChan := make(chan error)
	go func() {
		errChan <- srv.StreamMessages(stream)
	}()

	// Wait a bit for processing
	time.Sleep(100 * time.Millisecond)

	// Check if history was sent
	if len(stream.sentMsgs) == 0 {
		t.Fatal("Expected history messages, got none")
	}

	found := false
	for _, event := range stream.sentMsgs {
		if msg := event.GetMessage(); msg != nil {
			if msg.Content == "historical_msg" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("Did not find expected historical message")
	}

	// Close stream to stop handler
	close(stream.closeChan)
}

func TestBroadcast(t *testing.T) {
	// Setup
	hist := make(map[string]*history.ChannelBuffer)
	srv := NewIRCServiceServer(&pbConfig.Service{}, hist)

	// Mock Stream
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := NewMockStream(ctx)

	// Manually register stream (since StreamMessages blocks, we simulate registration)
	srv.mu.Lock()
	srv.streams.Store(stream, true)
	srv.mu.Unlock()

	// Broadcast
	msg := &pbService.IRCMessage{Content: "live_msg"}
	srv.Broadcast(msg)

	// Check receipt
	if len(stream.sentMsgs) != 1 {
		t.Errorf("Expected 1 broadcast message, got %d", len(stream.sentMsgs))
	} else {
		if stream.sentMsgs[0].GetMessage().GetContent() != "live_msg" {
			t.Errorf("Expected content 'live_msg', got %s", stream.sentMsgs[0].GetMessage().GetContent())
		}
	}
}

func TestSendMessage(t *testing.T) {
	srv := NewIRCServiceServer(nil, nil)
	resp, err := srv.SendMessage(context.Background(), &pbService.SendMessageRequest{})
	if err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}
	if resp.Success {
		t.Error("Expected Success to be false")
	}
	if resp.Error != "Not implemented" {
		t.Errorf("Expected 'Not implemented', got '%s'", resp.Error)
	}
}
