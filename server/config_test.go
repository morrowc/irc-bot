package main

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create temp config file
	content := `
irc: {
  host: "irc.test.net"
  port: 6667
  nick: "testbot"
  user: "testuser"
}
channels: {
  name: "#test"
  history_limit: 50
}
service: {
  port: 1234
}
tls: {
    ca_file: "ca.crt"
    cert_file: "server.crt"
    key_file: "server.key"
    client_cn: "client"
}
`
	tmpfile, err := ioutil.TempFile("", "config.textproto")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// Test loading
	cfg, err := loadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if cfg.GetIrc().GetHost() != "irc.test.net" {
		t.Errorf("Expected host irc.test.net, got %s", cfg.GetIrc().GetHost())
	}
	if len(cfg.GetChannels()) != 1 {
		t.Errorf("Expected 1 channel, got %d", len(cfg.GetChannels()))
	}
	if cfg.GetChannels()[0].GetName() != "#test" {
		t.Errorf("Expected channel #test, got %s", cfg.GetChannels()[0].GetName())
	}
}
