package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var (
	deviceMAC    string
	deviceIP     string
	devicePort   string
	ollamaTarget string
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
		listenAddr = ":11434"
	}

	if deviceMAC == "" || deviceIP == "" || devicePort == "" {
		return errors.New("DEVICE_MAC, DEVICE_IP and DEVICE_PORT must be set")
	}

	ollamaTarget = fmt.Sprintf("http://%s:%s", deviceIP, devicePort)

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

	log.Printf("Starting Ollama Waker proxy on %s -> %s", listenAddr, ollamaTarget)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
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

	// Proxy request to Ollama
	targetURL, _ := url.Parse(ollamaTarget)
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
		http.Error(w, "error contacting Ollama", http.StatusBadGateway)
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
	// Normalize and parse MAC
	clean := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(mac, ":", ""), "-", ""), ".", "")
	if len(clean) != 12 {
		return fmt.Errorf("mac should be 12 hex chars, got %q", mac)
	}
	macBytes, err := hex.DecodeString(clean)
	if err != nil {
		return fmt.Errorf("invalid mac: %v", err)
	}

	// Magic packet: 6 x 0xFF followed by 16 repetitions of MAC
	packet := make([]byte, 6+16*6)
	for i := range 6 {
		packet[i] = 0xFF
	}
	for i := range 16 {
		copy(packet[6+i*6:], macBytes)
	}

	// Try sending to the device's IP and to broadcast
	addrs := []string{deviceIP + ":9", "255.255.255.255:9"}
	var lastErr error
	for _, a := range addrs {
		udpAddr, err := net.ResolveUDPAddr("udp", a)
		if err != nil {
			lastErr = err
			continue
		}
		conn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			lastErr = err
			continue
		}
		// Set write deadline
		conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, err = conn.Write(packet)
		conn.Close()
		if err != nil {
			lastErr = err
			continue
		}
		// success
		return nil
	}
	return lastErr
}
