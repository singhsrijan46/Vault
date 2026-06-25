package network

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// PublicIPResponse represents the response from IP detection services
type PublicIPResponse struct {
	IP string `json:"ip"`
}

// GetPublicIP attempts to detect the public IP address using multiple methods
func GetPublicIP() (string, error) {
	// Try multiple services for redundancy
	services := []string{
		"https://api.ipify.org?format=json",
		"https://api.myip.com",
		"https://ifconfig.me/ip",
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	for _, service := range services {
		resp, err := client.Get(service)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		// Try to parse as JSON first
		var ipResp PublicIPResponse
		if err := json.Unmarshal(body, &ipResp); err == nil && ipResp.IP != "" {
			return ipResp.IP, nil
		}

		// If not JSON, treat as plain text IP
		ip := string(body)
		if net.ParseIP(ip) != nil {
			return ip, nil
		}
	}

	return "", fmt.Errorf("failed to detect public IP from all services")
}

// GetLocalIP returns the local network IP address
func GetLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// ParseListenAddr extracts the port from a listen address like ":3000" or "0.0.0.0:3000"
func ParseListenAddr(listenAddr string) (string, error) {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		// If no host, try prepending localhost
		if _, port, err = net.SplitHostPort("localhost" + listenAddr); err != nil {
			return "", fmt.Errorf("invalid listen address: %s", listenAddr)
		}
	}
	return port, nil
}

// BuildAdvertiseAddr creates an advertise address from IP and listen address
func BuildAdvertiseAddr(ip, listenAddr string) (string, error) {
	port, err := ParseListenAddr(listenAddr)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(ip, port), nil
}

// IsPrivateIP checks if an IP address is private (RFC 1918)
func IsPrivateIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Check for private IP ranges
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
	}

	for _, cidr := range privateRanges {
		_, subnet, _ := net.ParseCIDR(cidr)
		if subnet.Contains(parsedIP) {
			return true
		}
	}

	return false
}
