package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	DiscoverySchemaVersion = 1
	DiscoveryServiceName   = "ai-control-hud"
	discoveryProbe          = "AI_CONTROL_HUD_DISCOVER_V1"
)

type DiscoveryAnnouncement struct {
	Service       string `json:"service"`
	SchemaVersion int    `json:"schemaVersion"`
	HubID         string `json:"hubId"`
	Scheme        string `json:"scheme"`
	HTTPPort      int    `json:"httpPort"`
	HubVersion    string `json:"hubVersion"`
}

type DiscoveryResponder struct {
	conn      *net.UDPConn
	done      chan struct{}
	errMu     sync.Mutex
	serveErr  error
	closeOnce sync.Once
}

func StartDiscovery(ctx context.Context, config Config, version string) (*DiscoveryResponder, error) {
	if !config.DiscoveryEnabled {
		return nil, nil
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: config.DiscoveryPort})
	if err != nil {
		return nil, fmt.Errorf("listen for Hub discovery on UDP %d: %w", config.DiscoveryPort, err)
	}
	responder := &DiscoveryResponder{conn: conn, done: make(chan struct{})}
	announcement, err := json.Marshal(DiscoveryAnnouncement{
		Service:       DiscoveryServiceName,
		SchemaVersion: DiscoverySchemaVersion,
		HubID:         config.HubID,
		Scheme:        config.HTTPScheme,
		HTTPPort:      config.HTTPPort,
		HubVersion:    strings.TrimSpace(version),
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("encode Hub discovery announcement: %w", err)
	}
	go responder.serve(ctx, announcement)
	return responder, nil
}

func (d *DiscoveryResponder) serve(ctx context.Context, announcement []byte) {
	defer close(d.done)
	buffer := make([]byte, 512)
	for {
		_ = d.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, source, err := d.conn.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			d.setError(fmt.Errorf("read Hub discovery probe: %w", err))
			return
		}
		if string(buffer[:n]) != discoveryProbe {
			continue
		}
		if _, err := d.conn.WriteToUDP(announcement, source); err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			d.setError(fmt.Errorf("reply to Hub discovery probe: %w", err))
			return
		}
	}
}

func (d *DiscoveryResponder) Wait() error {
	if d == nil {
		return nil
	}
	<-d.done
	d.errMu.Lock()
	defer d.errMu.Unlock()
	return d.serveErr
}

func (d *DiscoveryResponder) Close() error {
	if d == nil {
		return nil
	}
	var err error
	d.closeOnce.Do(func() { err = d.conn.Close() })
	return err
}

func (d *DiscoveryResponder) setError(err error) {
	d.errMu.Lock()
	defer d.errMu.Unlock()
	d.serveErr = err
}
