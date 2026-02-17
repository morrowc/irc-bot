package history

import (
	"sync"
	"time"

	pb "github.com/morrowc/irc-bot/proto/service"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ChannelBuffer manages history for a single channel.
type ChannelBuffer struct {
	mu       sync.RWMutex
	messages []*pb.IRCMessage
	limit    int
}

// NewChannelBuffer creates a new buffer with the given limit.
func NewChannelBuffer(limit int) *ChannelBuffer {
	if limit < 0 {
		limit = 0
	}
	return &ChannelBuffer{
		messages: make([]*pb.IRCMessage, 0, limit),
		limit:    limit,
	}
}

// Add appends a message to the buffer, dropping old ones if limit is reached.
func (cb *ChannelBuffer) Add(msg *pb.IRCMessage) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.limit == 0 {
		return
	}

	if len(cb.messages) >= cb.limit {
		// Drop the oldest message
		cb.messages = cb.messages[1:]
	}
	cb.messages = append(cb.messages, msg)
}

// GetSince returns all messages since the given timestamp.
func (cb *ChannelBuffer) GetSince(since time.Time) []*pb.IRCMessage {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	var result []*pb.IRCMessage
	for _, msg := range cb.messages {
		if msg.GetTimestamp().AsTime().After(since) {
			result = append(result, msg)
		}
	}
	return result
}
