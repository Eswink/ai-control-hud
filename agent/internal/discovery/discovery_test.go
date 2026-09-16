package discovery

import (
	"net"
	"testing"
)

func TestParseResponseUsesResponderAddress(t *testing.T) {
	payload := []byte(`{"service":"ai-control-hud","schemaVersion":1,"hubId":"central-hub","scheme":"http","httpPort":8787,"hubVersion":"test"}`)
	result, err := parseResponse(payload, &net.UDPAddr{IP: net.ParseIP("192.168.101.103"), Port: 8788})
	if err != nil {
		t.Fatal(err)
	}
	if result.BaseURL != "http://192.168.101.103:8787" {
		t.Fatalf("unexpected base URL %q", result.BaseURL)
	}
	if result.HubID != "central-hub" {
		t.Fatalf("unexpected hub id %q", result.HubID)
	}
}

func TestParseResponseRejectsWrongProtocol(t *testing.T) {
	payload := []byte(`{"service":"other","schemaVersion":1,"hubId":"central-hub","scheme":"http","httpPort":8787}`)
	if _, err := parseResponse(payload, &net.UDPAddr{IP: net.ParseIP("192.168.101.103"), Port: 8788}); err == nil {
		t.Fatal("expected protocol mismatch")
	}
}

func TestBroadcastIPv4(t *testing.T) {
	_, network, err := net.ParseCIDR("192.168.101.23/24")
	if err != nil {
		t.Fatal(err)
	}
	network.IP = net.ParseIP("192.168.101.23")
	if got := broadcastIPv4(network); got != "192.168.101.255" {
		t.Fatalf("unexpected broadcast %q", got)
	}
}

func TestBroadcastIPv4SkipsPointToPoint(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.1/31")
	if err != nil {
		t.Fatal(err)
	}
	network.IP = net.ParseIP("10.0.0.1")
	if got := broadcastIPv4(network); got != "" {
		t.Fatalf("expected no broadcast for /31, got %q", got)
	}
}
