package history

import (
	"testing"
	"time"

	pbService "github.com/morrowc/irc-bot/proto/service"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestChannelBuffer(t *testing.T) {
	limit := 5
	cb := NewChannelBuffer(limit)

	// Test Add and Limit
	for i := 0; i < limit+2; i++ {
		cb.Add(&pbService.IRCMessage{
			Content:   "msg",
			Timestamp: timestamppb.Now(),
		})
	}

	msgs := cb.GetSince(time.Time{})
	if len(msgs) != limit {
		t.Errorf("Expected %d messages, got %d", limit, len(msgs))
	}

	// Test GetSince
	now := time.Now()
	cb.Add(&pbService.IRCMessage{
		Content:   "new_msg",
		Timestamp: timestamppb.New(now.Add(1 * time.Second)),
	})

	// Get messages after 'now'
	recentMsgs := cb.GetSince(now)
	if len(recentMsgs) != 1 {
		t.Errorf("Expected 1 recent message, got %d", len(recentMsgs))
	}
	if recentMsgs[0].Content != "new_msg" {
		t.Errorf("Expected content 'new_msg', got '%s'", recentMsgs[0].Content)
	}
}
