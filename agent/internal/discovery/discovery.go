package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

const (
	DefaultPort       = 8788
	SchemaVersion     = 1
	ServiceName       = "ai-control-hud"
	requestPayload    = "AI_CONTROL_HUD_DISCOVER_V1"
	defaultDiscoverIn = 1500 * time.Millisecond
	readSlice         = 250 * time.Millisecond
)

type announcement struct {
	Service       string `json:"service"`
	SchemaVersion int    `json:"schemaVersion"`
	HubID         string `json:"hubId"`
	Scheme        string `json:"scheme"`
	HTTPPort      int    `json:"httpPort"`
	HubVersion    string `json:"hubVersion"`
}

type Result struct {
	BaseURL    string
	HubID      string
	HubVersion string
	Address    string
}

func Discover(ctx context.Context, port int) (Result, error) {
	if port < 1 || port > 65535 {
		return Result{}, errors.New("discovery port must be within 1..65535")
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return Result{}, fmt.Errorf("open hub discovery socket: %w", err)
	}
	defer conn.Close()
	if err := enableBroadcast(conn); err != nil {
		return Result{}, fmt.Errorf("enable hub discovery broadcast: %w", err)
	}

	destinations := broadcastDestinations(port)
	payload := []byte(requestPayload)
	sent := 0
	for _, destination := range destinations {
		if _, err := conn.WriteToUDP(payload, destination); err == nil {
			sent++
		}
	}
	if sent == 0 {
		return Result{}, errors.New("hub discovery broadcast could not be sent")
	}

	deadline := time.Now().Add(defaultDiscoverIn)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	buffer := make([]byte, 2048)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		readDeadline := time.Now().Add(readSlice)
		if deadline.Before(readDeadline) {
			readDeadline = deadline
		}
		if err := conn.SetReadDeadline(readDeadline); err != nil {
			return Result{}, fmt.Errorf("set hub discovery deadline: %w", err)
		}
		n, source, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return Result{}, fmt.Errorf("read hub discovery response: %w", err)
		}
		result, err := parseResponse(buffer[:n], source)
		if err == nil {
			return result, nil
		}
	}
	return Result{}, errors.New("no AI Control Hub discovered on the local network")
}

func parseResponse(data []byte, source *net.UDPAddr) (Result, error) {
	var response announcement
	if err := json.Unmarshal(data, &response); err != nil {
		return Result{}, err
	}
	if response.Service != ServiceName || response.SchemaVersion != SchemaVersion {
		return Result{}, errors.New("unsupported hub discovery response")
	}
	response.HubID = strings.TrimSpace(response.HubID)
	response.Scheme = strings.ToLower(strings.TrimSpace(response.Scheme))
	if response.HubID == "" {
		return Result{}, errors.New("hub discovery response is missing hub id")
	}
	if response.Scheme != "http" && response.Scheme != "https" {
		return Result{}, errors.New("hub discovery response has invalid scheme")
	}
	if response.HTTPPort < 1 || response.HTTPPort > 65535 {
		return Result{}, errors.New("hub discovery response has invalid HTTP port")
	}
	if source == nil || source.IP == nil || source.IP.To4() == nil {
		return Result{}, errors.New("hub discovery response has invalid source address")
	}
	address := source.IP.To4().String()
	return Result{
		BaseURL:    fmt.Sprintf("%s://%s:%d", response.Scheme, address, response.HTTPPort),
		HubID:      response.HubID,
		HubVersion: strings.TrimSpace(response.HubVersion),
		Address:    address,
	}, nil
}

func broadcastDestinations(port int) []*net.UDPAddr {
	seen := map[string]bool{}
	addresses := []string{"255.255.255.255"}
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, address := range addrs {
				ipNet, ok := address.(*net.IPNet)
				if !ok {
					continue
				}
				broadcast := broadcastIPv4(ipNet)
				if broadcast != "" {
					addresses = append(addresses, broadcast)
				}
			}
		}
	}
	sort.Strings(addresses)
	result := make([]*net.UDPAddr, 0, len(addresses))
	for _, address := range addresses {
		if seen[address] {
			continue
		}
		seen[address] = true
		ip := net.ParseIP(address).To4()
		if ip == nil {
			continue
		}
		result = append(result, &net.UDPAddr{IP: ip, Port: port})
	}
	return result
}

func broadcastIPv4(network *net.IPNet) string {
	if network == nil {
		return ""
	}
	ip := network.IP.To4()
	mask := network.Mask
	if ip == nil || len(mask) != net.IPv4len {
		return ""
	}
	ones, bits := mask.Size()
	if bits != 32 || ones < 0 || ones >= 31 {
		return ""
	}
	broadcast := make(net.IP, net.IPv4len)
	for i := 0; i < net.IPv4len; i++ {
		broadcast[i] = ip[i] | ^mask[i]
	}
	return broadcast.String()
}
