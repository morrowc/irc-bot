package main

import (
	"crypto/tls"
	"sync"

	"github.com/lrstanley/girc"
	"github.com/morrowc/irc-bot/server/history"
	"google.golang.org/protobuf/types/known/timestamppb"

	pbConfig "github.com/morrowc/irc-bot/proto/config"
	pbService "github.com/morrowc/irc-bot/proto/service"
)

type IRCBot struct {
	client    *girc.Client
	history   func(channel string) *history.ChannelBuffer
	broadcast func(msg *pbService.IRCMessage)
	// State
	mu       sync.RWMutex
	channels map[string]string // channel -> key
}

func NewIRCBot(cfg *pbConfig.IRCServer, channels []*pbConfig.Channel, histGetter func(string) *history.ChannelBuffer, broadcaster func(*pbService.IRCMessage)) *IRCBot {
	// Basic setup config
	config := girc.Config{
		Server:     cfg.GetHost(),
		Port:       int(cfg.GetPort()),
		Nick:       cfg.GetNick(),
		User:       cfg.GetUser(),
		Name:       cfg.GetUser(),
		ServerPass: cfg.GetPassword(),
		SSL:        cfg.GetUseTls(),
	}

	if !cfg.GetUseTls() {
		config.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}

	client := girc.New(config)

	bot := &IRCBot{
		client:    client,
		history:   histGetter,
		broadcast: broadcaster,
		channels:  make(map[string]string),
	}

	for _, ch := range channels {
		bot.channels[ch.GetName()] = ch.GetKey()
	}

	client.Handlers.Add(girc.PRIVMSG, bot.handlePrivMsg)
	client.Handlers.Add(girc.JOIN, bot.handleJoin)
	client.Handlers.Add(girc.CONNECTED, func(c *girc.Client, e girc.Event) {
		bot.mu.RLock()
		defer bot.mu.RUnlock()
		for ch, key := range bot.channels {
			c.Cmd.JoinKey(ch, key)
		}
	})

	return bot
}

func (b *IRCBot) UpdateChannels(newChannels []*pbConfig.Channel) {
	b.mu.Lock()
	defer b.mu.Unlock()

	newMap := make(map[string]string)
	for _, ch := range newChannels {
		newMap[ch.GetName()] = ch.GetKey()
	}

	// Calculate difference
	// To Join
	for ch, key := range newMap {
		if _, exists := b.channels[ch]; !exists {
			if b.client.IsConnected() {
				b.client.Cmd.JoinKey(ch, key)
			}
		}
	}

	// To Part
	for ch := range b.channels {
		if _, exists := newMap[ch]; !exists {
			if b.client.IsConnected() {
				b.client.Cmd.Part(ch)
			}
		}
	}

	b.channels = newMap
}

func (b *IRCBot) Connect() error {
	return b.client.Connect()
}

func (b *IRCBot) Close() {
	b.client.Close()
}

func (b *IRCBot) Join(channel, key string) {
	b.client.Cmd.JoinKey(channel, key)
}

func (b *IRCBot) Send(channel, message string) {
	b.client.Cmd.Message(channel, message)

	// Echo back to history/clients so the sender sees it too
	msg := &pbService.IRCMessage{
		Timestamp: timestamppb.Now(),
		Channel:   channel,
		Sender:    b.client.GetNick(),
		Content:   message,
	}

	// Store in history
	if buf := b.history(channel); buf != nil {
		buf.Add(msg)
	}

	// Broadcast to gRPC clients
	b.broadcast(msg)
}

func (b *IRCBot) handlePrivMsg(c *girc.Client, e girc.Event) {
	channel := e.Params[0]
	content := e.Last()
	sender := e.Source.Name

	msg := &pbService.IRCMessage{
		Timestamp: timestamppb.Now(),
		Channel:   channel,
		Sender:    sender,
		Content:   content,
	}

	// Store in history
	if buf := b.history(channel); buf != nil {
		buf.Add(msg)
	}

	// Broadcast to gRPC clients
	b.broadcast(msg)
}

func (b *IRCBot) handleJoin(c *girc.Client, e girc.Event) {
	// Handle join events if needed (maybe system message?)
}
