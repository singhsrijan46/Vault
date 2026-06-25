package network

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

// DebugLog is a simple debug logging function for the network package
func DebugLog(format string, args ...interface{}) {
	// For now, just use regular logging
	// This could be enhanced with a debug flag later
	log.Printf("[DEBUG] "+format, args...)
}

const (
	// mDNS service type for PeerVault
	ServiceType = "_peervault._tcp"
	// Domain for mDNS
	ServiceDomain = "local."
)

// DiscoveryService handles peer discovery via mDNS
type DiscoveryService struct {
	serviceName     string
	port            int
	advertiseAddr   string
	server          *mdns.Server
	onPeerFound     func(string) error
	discoveredPeers map[string]time.Time
	peerLock        sync.RWMutex
	stopCh          chan struct{}
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewDiscoveryService creates a new mDNS discovery service
func NewDiscoveryService(serviceName string, port int, advertiseAddr string) *DiscoveryService {
	ctx, cancel := context.WithCancel(context.Background())
	return &DiscoveryService{
		serviceName:     serviceName,
		port:            port,
		advertiseAddr:   advertiseAddr,
		discoveredPeers: make(map[string]time.Time),
		stopCh:          make(chan struct{}),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start begins advertising and discovering peers via mDNS
func (ds *DiscoveryService) Start() error {
	// Start advertising this node
	if err := ds.startAdvertising(); err != nil {
		return fmt.Errorf("failed to start mDNS advertising: %w", err)
	}

	// Start discovering other nodes
	go ds.startDiscovery()

	log.Printf("mDNS discovery started: advertising as %s on port %d", ds.serviceName, ds.port)
	return nil
}

// Stop stops the discovery service
func (ds *DiscoveryService) Stop() {
	ds.cancel()
	close(ds.stopCh)
	if ds.server != nil {
		ds.server.Shutdown()
	}
	log.Println("mDNS discovery stopped")
}

// SetPeerFoundCallback sets the callback for when a peer is discovered
func (ds *DiscoveryService) SetPeerFoundCallback(callback func(string) error) {
	ds.onPeerFound = callback
}

// startAdvertising advertises this node on the local network
func (ds *DiscoveryService) startAdvertising() error {
	// Get hostname
	hostname, err := ds.getHostname()
	if err != nil {
		hostname = "peervault-node"
	}

	// Get local IPs
	ips, err := ds.getLocalIPs()
	if err != nil {
		return err
	}

	// Create mDNS service
	service, err := mdns.NewMDNSService(
		hostname,
		ServiceType,
		ServiceDomain,
		"",
		ds.port,
		ips,
		[]string{fmt.Sprintf("version=1.0"), fmt.Sprintf("addr=%s", ds.advertiseAddr)},
	)
	if err != nil {
		return err
	}

	// Create and start mDNS server
	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return err
	}

	ds.server = server
	return nil
}

// startDiscovery continuously discovers peers on the local network
func (ds *DiscoveryService) startDiscovery() {
	// Initial discovery
	ds.discoverPeers()

	// Periodic rediscovery every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ds.discoverPeers()
		case <-ds.stopCh:
			return
		}
	}
}

// discoverPeers performs a single discovery scan
func (ds *DiscoveryService) discoverPeers() {
	// Create entries channel
	entriesCh := make(chan *mdns.ServiceEntry, 10)

	// Start discovery goroutine
	go func() {
		defer close(entriesCh)

		// Query for PeerVault services
		params := &mdns.QueryParam{
			Service:             ServiceType,
			Domain:              ServiceDomain,
			Timeout:             3 * time.Second,
			Entries:             entriesCh,
			WantUnicastResponse: false,
		}

		if err := mdns.Query(params); err != nil {
			DebugLog("mDNS query error: %v", err)
		}
	}()

	// Process discovered entries
	for entry := range entriesCh {
		ds.handleDiscoveredPeer(entry)
	}
}

// handleDiscoveredPeer processes a discovered peer
func (ds *DiscoveryService) handleDiscoveredPeer(entry *mdns.ServiceEntry) {
	// Skip if it's our own service
	if entry.Port == ds.port {
		// Additional check: compare IPs
		localIPs, _ := ds.getLocalIPs()
		for _, localIP := range localIPs {
			if entry.AddrV4 != nil && entry.AddrV4.Equal(localIP) {
				return
			}
			if entry.AddrV6 != nil && entry.AddrV6.Equal(localIP) {
				return
			}
		}
	}

	// Determine peer address
	var peerAddr string
	if entry.AddrV4 != nil {
		peerAddr = fmt.Sprintf("%s:%d", entry.AddrV4.String(), entry.Port)
	} else if entry.AddrV6 != nil {
		peerAddr = fmt.Sprintf("[%s]:%d", entry.AddrV6.String(), entry.Port)
	} else {
		return
	}

	// Check if we've already discovered this peer recently
	ds.peerLock.Lock()
	lastSeen, exists := ds.discoveredPeers[peerAddr]
	if exists && time.Since(lastSeen) < 5*time.Minute {
		ds.peerLock.Unlock()
		return
	}
	ds.discoveredPeers[peerAddr] = time.Now()
	ds.peerLock.Unlock()

	log.Printf("Discovered peer via mDNS: %s (%s)", peerAddr, entry.Name)

	// Notify callback
	if ds.onPeerFound != nil {
		go func() {
			if err := ds.onPeerFound(peerAddr); err != nil {
				DebugLog("Failed to connect to discovered peer %s: %v", peerAddr, err)
			} else {
				log.Printf("Successfully connected to peer %s discovered via mDNS", peerAddr)
			}
		}()
	}
}

// getHostname returns the system hostname
func (ds *DiscoveryService) getHostname() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	// Sanitize hostname for mDNS (no spaces or special chars)
	hostname = strings.ReplaceAll(hostname, " ", "-")
	return hostname, nil
}

// getLocalIPs returns all non-loopback local IP addresses
func (ds *DiscoveryService) getLocalIPs() ([]net.IP, error) {
	var ips []net.IP

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		// Skip down interfaces
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Skip loopback
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Skip loopback addresses
			if ip == nil || ip.IsLoopback() {
				continue
			}

			ips = append(ips, ip)
		}
	}

	return ips, nil
}

// GetDiscoveredPeers returns the list of discovered peers
func (ds *DiscoveryService) GetDiscoveredPeers() []string {
	ds.peerLock.RLock()
	defer ds.peerLock.RUnlock()

	peers := make([]string, 0, len(ds.discoveredPeers))
	for peer := range ds.discoveredPeers {
		peers = append(peers, peer)
	}
	return peers
}

// CleanupOldPeers removes peers not seen in the last 10 minutes
func (ds *DiscoveryService) CleanupOldPeers() {
	ds.peerLock.Lock()
	defer ds.peerLock.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	for peer, lastSeen := range ds.discoveredPeers {
		if lastSeen.Before(cutoff) {
			delete(ds.discoveredPeers, peer)
			DebugLog("Removed stale peer from discovery cache: %s", peer)
		}
	}
}
