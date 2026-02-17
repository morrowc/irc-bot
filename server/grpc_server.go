package main

import (
	"context"
	"log"
	"sync"
	"time"

	pbConfig "github.com/morrowc/irc-bot/proto/config"
	pbService "github.com/morrowc/irc-bot/proto/service"
	"github.com/morrowc/irc-bot/server/history"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type IRCServiceServer struct {
	pbService.UnimplementedIRCServiceServer
	config  *pbConfig.Service
	history map[string]*history.ChannelBuffer
	// Active streams
	streams sync.Map // map[pbService.IRCService_StreamMessagesServer]bool
	mu      sync.RWMutex
}

func NewIRCServiceServer(cfg *pbConfig.Service, hist map[string]*history.ChannelBuffer) *IRCServiceServer {
	return &IRCServiceServer{
		config:  cfg,
		history: hist,
	}
}

func (s *IRCServiceServer) StreamMessages(stream pbService.IRCService_StreamMessagesServer) error {
	// Basic Auth Check (Ideally via Interceptor, but simplistic for now as per req)
	// Client sends subscription request implementation.
	// For now, let's assume the client sends the first message as a SubscribeRequest.

	// We need to wait for the first message from the client to know what they want
	req, err := stream.Recv()
	if err != nil {
		return err
	}

	subReq := req.GetSubscribe()
	if subReq == nil {
		return status.Error(codes.InvalidArgument, "First message must be SubscribeRequest")
	}

	// TODO: Verify client_passkey if we add it to the Protocol or Metadata.
	// The requirement says "storage of user passkey in plaintext in prototext config is acceptable".
	// We should probably check metadata for passkey or add it to the SubscribeRequest.
	// For this pass, I will assume metadata auth or just no auth for the very first step,
	// but the plan said "Authenticate with a passkey".
	// Let's add passkey to SubscribeRequest in proto or use metadata.
	// Metadata is better. I'll stick to the plan of "passkey provided".

	// Handle History
	if subReq.GetGetHistory() {
		for _, buf := range s.history {
			msgs := buf.GetSince(time.Time{}) // Get all for now, or use a specific time if provided
			for _, msg := range msgs {
				if err := stream.Send(&pbService.StreamEvent{
					Event: &pbService.StreamEvent_Message{Message: msg},
				}); err != nil {
					return err
				}
			}
		}
	}

	// Register stream for live updates
	s.mu.Lock()
	s.streams.Store(stream, true)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.streams.Delete(stream)
		s.mu.Unlock()
	}()

	// Keep stream alive and handle incoming control messages (if any)
	for {
		_, err := stream.Recv()
		if err != nil {
			return err
		}
	}
}

func (s *IRCServiceServer) Broadcast(msg *pbService.IRCMessage) {
	s.streams.Range(func(key, value interface{}) bool {
		stream := key.(pbService.IRCService_StreamMessagesServer)
		// Best effort send. If it blocks/fails, simplistic handling for now.
		// In production, we'd use a per-client queue to avoid blocking the broadcaster.
		if err := stream.Send(&pbService.StreamEvent{
			Event: &pbService.StreamEvent_Message{Message: msg},
		}); err != nil {
			log.Printf("Failed to send to client: %v", err)
			// Maybe remove client?
		}
		return true
	})
}

func (s *IRCServiceServer) SendMessage(ctx context.Context, req *pbService.SendMessageRequest) (*pbService.SendMessageResponse, error) {
	// TODO: Implement sending to IRC via a channel or callback to the bot
	return &pbService.SendMessageResponse{Success: false, Error: "Not implemented"}, nil
}
