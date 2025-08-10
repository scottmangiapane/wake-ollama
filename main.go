package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

var (
	deviceMAC    string
	deviceIP     string
	devicePort   string
	target       string
	listenAddr   string
	pollInterval time.Duration
	wakeTimeout  time.Duration
)

func initConfig() error {
	deviceMAC = os.Getenv("DEVICE_MAC")
	deviceIP = os.Getenv("DEVICE_IP")
	devicePort = os.Getenv("DEVICE_PORT")
	listenAddr = os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = "11434"
	}

	if deviceMAC == "" || deviceIP == "" || devicePort == "" {
		return errors.New("DEVICE_MAC, DEVICE_IP and DEVICE_PORT must be set")
	}

	target = fmt.Sprintf("http://%s:%s", deviceIP, devicePort)

	pi := os.Getenv("POLL_INTERVAL_SEC")
	if pi == "" {
		pollInterval = 2 * time.Second
	} else {
		secs, err := time.ParseDuration(pi + "s")
		if err != nil {
			pollInterval = 2 * time.Second
		} else {
			pollInterval = secs
		}
	}

	tw := os.Getenv("WAKE_TIMEOUT_SEC")
	if tw == "" {
		wakeTimeout = 120 * time.Second
	} else {
		secs, err := time.ParseDuration(tw + "s")
		if err != nil {
			wakeTimeout = 120 * time.Second
		} else {
			wakeTimeout = secs
		}
	}

	log.Printf("Configured: DEVICE_MAC=%s DEVICE_IP=%s DEVICE_PORT=%s LISTEN_ADDR=%s", deviceMAC, deviceIP, devicePort, listenAddr)
	return nil
}

func main() {
	if err := initConfig(); err != nil {
		log.Fatalf("config error: %v", err)
	}

	http.HandleFunc("/", wakeAndProxyHandler)

	log.Printf("Starting proxy on %s -> %s", listenAddr, target)
	if err := http.ListenAndServe(":"+listenAddr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func wakeAndProxyHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// If device is not up, wake it and wait
	if !isUp(deviceIP, devicePort) {
		log.Printf("device %s appears down; sending WoL", deviceIP)
		if err := sendMagicPacket(deviceMAC); err != nil {
			log.Printf("failed to send magic packet: %v", err)
			// don't fail yet — still try to proxy which will likely fail
		} else {
			log.Printf("magic packet sent to %s", deviceMAC)
		}

		// wait until target is reachable or timeout
		deadline := time.Now().Add(wakeTimeout)
		for {
			if isUp(deviceIP, devicePort) {
				break
			}
			if time.Now().After(deadline) {
				log.Printf("timeout waiting for device to come up")
				http.Error(w, "timeout waiting for device to wake", http.StatusGatewayTimeout)
				return
			}
			select {
			case <-ctx.Done():
				log.Printf("request cancelled while waiting for device")
				http.Error(w, "client cancelled", http.StatusRequestTimeout)
				return
			case <-time.After(pollInterval):
				// loop
			}
		}
	}

	// Proxy request to target device
	targetURL, _ := url.Parse(target)
	proxyTo := targetURL.String() + r.URL.Path
	if r.URL.RawQuery != "" {
		proxyTo += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(ctx, r.Method, proxyTo, r.Body)
	if err != nil {
		log.Printf("failed to create proxy request: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Copy relevant headers (but don't forward Host)
	for name, values := range r.Header {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	req.Host = targetURL.Host

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("error forwarding request: %v", err)
		http.Error(w, "error contacting target device", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// isUp checks if ip:port accepts TCP connections
func isUp(ip, port string) bool {
	addr := net.JoinHostPort(ip, port)
	timeout := 1 * time.Second
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// sendMagicPacket crafts and sends a WoL magic packet to the broadcast address
func sendMagicPacket(mac string) error {
	hwAddr, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("invalid MAC address: %w", err)
	}

	// Build magic packet: 6x 0xFF followed by 16x MAC address
	packet := make([]byte, 6+16*len(hwAddr))
	for i := 0; i < 6; i++ {
		packet[i] = 0xFF
	}
	for i := 6; i < len(packet); i += len(hwAddr) {
		copy(packet[i:], hwAddr)
	}

	// Broadcast address + standard WOL UDP port 9
	addr := &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: 9,
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to dial UDP: %w", err)
	}
	defer conn.Close()

	// Enable broadcast (needed on some systems)
	if err := conn.SetWriteBuffer(len(packet)); err != nil {
		return fmt.Errorf("failed to set write buffer: %w", err)
	}

	_, err = conn.Write(packet)
	return err
}
