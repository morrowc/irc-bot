package main

import (
	"log"
	"time"

	"github.com/lrstanley/girc"
	pbConfig "github.com/morrowc/irc-bot/proto/config"
    pbService "github.com/morrowc/irc-bot/proto/service"
    "github.com/morrowc/irc-bot/server/history"
    "google.golang.org/protobuf/types/known/timestamppb"
)

type IRCBot struct {
	client *girc.Client
    history func(channel string) *history.ChannelBuffer
    broadcast func(msg *pbService.IRCMessage)
}

func NewIRCBot(cfg *pbConfig.IRCServer, channels []*pbConfig.Channel, histGetter func(string) *history.ChannelBuffer, broadcaster func(*pbService.IRCMessage)) *IRCBot {
    // Basic setup config
    config := girc.Config{
        Server: cfg.GetHost(),
        Port:   int(cfg.GetPort()),
        Nick:   cfg.GetNick(),
        User:   cfg.GetUser(),
        Name:   cfg.GetUser(),
        Pass:   cfg.GetPassword(),
        SSL:    cfg.GetUseTls(),
    }

	client := girc.New(config)
    
    bot := &IRCBot{
        client: client,
        history: histGetter,
        broadcast: broadcaster,
    }

	client.Handlers.Add(girc.PRIVMSG, bot.handlePrivMsg)
    client.Handlers.Add(girc.JOIN, bot.handleJoin)
    client.Handlers.Add(girc.CONNECTED, func(c *girc.Client, e girc.Event) {
        for _, ch := range channels {
            key := ch.GetKey()
            c.Cmd.JoinKey(ch.GetName(), key)
        }
    })

	return bot
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
