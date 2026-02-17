package main

import (
	"bytes"
	"strings"
	"testing"

	pbService "github.com/morrowc/irc-bot/proto/service"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHandleMessage(t *testing.T) {
	out := new(bytes.Buffer)
	cs := NewClientState()
	cs.out = out
	cs.width = 80
	cs.height = 24

	msg := &pbService.IRCMessage{
		Channel:   "#test",
		Sender:    "sender",
		Content:   "hello",
		Timestamp: timestamppb.Now(),
	}

	cs.handleMessage(msg)

	// Verify history
	if len(cs.msgHistory["#test"]) != 1 {
		t.Errorf("Expected 1 message in history, got %d", len(cs.msgHistory["#test"]))
	}

	// Verify output (current channel auto-set)
	if cs.currentChannel != "#test" {
		t.Errorf("Expected current channel #test, got %s", cs.currentChannel)
	}

	// Check output contains message
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("Expected output to contain 'hello', got %s", out.String())
	}
}

func TestChannelSwitching(t *testing.T) {
	out := new(bytes.Buffer)
	cs := NewClientState()
	cs.out = out
	cs.width = 80
	cs.height = 24

	// Add messages for two channels
	cs.handleMessage(&pbService.IRCMessage{Channel: "#chan1", Content: "msg1", Timestamp: timestamppb.Now()})
	cs.handleMessage(&pbService.IRCMessage{Channel: "#chan2", Content: "msg2", Timestamp: timestamppb.Now()})

	// Initial channel should be #chan1
	if cs.currentChannel != "#chan1" {
		t.Errorf("Expected #chan1, got %s", cs.currentChannel)
	}

	// Next Channel -> #chan2
	out.Reset()
	cs.nextChannel()
	if cs.currentChannel != "#chan2" {
		t.Errorf("Expected #chan2, got %s", cs.currentChannel)
	}
	if !strings.Contains(out.String(), "Switched to #chan2") {
		t.Errorf("Expected switch message, got %s", out.String())
	}

	// Next Channel -> #chan1 (loop)
	cs.nextChannel()
	if cs.currentChannel != "#chan1" {
		t.Errorf("Expected #chan1, got %s", cs.currentChannel)
	}

	// Prev Channel -> #chan2 (loop)
	cs.prevChannel()
	if cs.currentChannel != "#chan2" {
		t.Errorf("Expected #chan2, got %s", cs.currentChannel)
	}
}

func TestHandleInput(t *testing.T) {
	out := new(bytes.Buffer)
	cs := NewClientState()
	cs.out = out
	cs.width = 80
	cs.height = 24
	cs.channels = []string{"#chan1", "#chan2"}
	cs.currentChannel = "#chan1"

	// Mock exit
	exitCalled := false
	cs.exitFunc = func(code int) {
		exitCalled = true
	}

	// Simulate input: Next Channel (Ctrl-N=14), 'a', Backspace (127), 'b', Enter (10), Ctrl-D (4)
	input := []byte{14, 'a', 127, 'b', 10, 4}
	r := bytes.NewReader(input)

	cs.handleInput(r)

	// Check if exit was called
	if !exitCalled {
		t.Error("Expected exitFunc to be called via Ctrl-D")
	}

	// Check effects
	if cs.currentChannel != "#chan2" {
		t.Errorf("Expected switch to #chan2, got %s", cs.currentChannel)
	}

	if !strings.Contains(out.String(), "Switched to #chan2") {
		t.Error("Expected redraw output")
	}
	if !strings.Contains(out.String(), "b") {
		t.Error("Expected echoed character 'b'")
	}
}

func TestHandleCommand(t *testing.T) {
	out := new(bytes.Buffer)
	exitCalled := false
	cs := NewClientState()
	cs.out = out
	cs.exitFunc = func(code int) {
		exitCalled = true
	}
	cs.width = 80
	cs.height = 24

	// Test /disconnect
	cs.handleCommand("/disconnect")
	if !exitCalled {
		t.Error("Expected exit logic for /disconnect")
	}
}
