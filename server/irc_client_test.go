package main

import (
	"testing"
	"time"

	"github.com/lrstanley/girc"
	pbService "github.com/morrowc/irc-bot/proto/service"
	"github.com/morrowc/irc-bot/server/history"
)

func TestHandlePrivMsg(t *testing.T) {
	// Mocks
	var storedMsg *pbService.IRCMessage
	historyFunc := func(channel string) *history.ChannelBuffer {
		if channel != "#test" {
			t.Errorf("Expected #test, got %s", channel)
		}
		return history.NewChannelBuffer(10)
	}

	broadcastFunc := func(msg *pbService.IRCMessage) {
		storedMsg = msg
	}

	bot := &IRCBot{
		client:    nil, // Not used in handlePrivMsg
		history:   historyFunc,
		broadcast: broadcastFunc,
	}

	// Create Event
	event := girc.Event{
		Command:   girc.PRIVMSG,
		Params:    []string{"#test", "hello world"},
		Source:    &girc.Source{Name: "sender_nick"},
		Timestamp: time.Now(),
	}

	// Call Handler
	bot.handlePrivMsg(nil, event)

	// Verify
	if storedMsg == nil {
		t.Fatal("Broadcast not called")
	}
	if storedMsg.Content != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", storedMsg.Content)
	}
	if storedMsg.Sender != "sender_nick" {
		t.Errorf("Expected 'sender_nick', got '%s'", storedMsg.Sender)
	}
}

func TestHandleJoin(t *testing.T) {
	bot := &IRCBot{}
	// Should not panic
	bot.handleJoin(nil, girc.Event{})
}
