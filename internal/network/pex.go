package network

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/AdityaKrSingh26/PeerVault/pkg/p2p"
)

// PeerInfo represents information about a peer
type PeerInfo struct {
	Address  string    `json:"address"`
	LastSeen time.Time `json:"last_seen"`
	Source   string    `json:"source"` // "bootstrap", "mdns", "pex"
}

// MessagePeerExchange contains a list of known peers
type MessagePeerExchange struct {
	Peers []PeerInfo `json:"peers"`
}

// PeerExchangeService manages peer discovery via peer exchange
type PeerExchangeService struct {
	knownPeers       map[string]*PeerInfo
	peerLock         sync.RWMutex
	server           *FileServer
	Enabled          bool
	exchangeInterval time.Duration
	stopCh           chan struct{}
}

// NewPeerExchangeService creates a new PEX service
func NewPeerExchangeService(server *FileServer) *PeerExchangeService {
	return &PeerExchangeService{
		knownPeers:       make(map[string]*PeerInfo),
		server:           server,
		Enabled:          false,
		exchangeInterval: 5 * time.Minute, // Exchange peer lists every 5 minutes
		stopCh:           make(chan struct{}),
	}
}

// Start enables peer exchange
func (pex *PeerExchangeService) Start() {
	pex.Enabled = true
	log.Println("Peer exchange (PEX) enabled")

	// Start periodic peer list exchange
	go pex.periodicExchange()

	// Start cleanup of old peers
	go pex.periodicCleanup()
}

// Stop disables peer exchange
func (pex *PeerExchangeService) Stop() {
	pex.Enabled = false
	close(pex.stopCh)
	log.Println("Peer exchange (PEX) disabled")
}

// AddKnownPeer adds a peer to the known peers list
func (pex *PeerExchangeService) AddKnownPeer(address string, source string) {
	if !pex.Enabled {
		return
	}

	pex.peerLock.Lock()
	defer pex.peerLock.Unlock()

	// Update or add peer
	if peer, exists := pex.knownPeers[address]; exists {
		peer.LastSeen = time.Now()
	} else {
		pex.knownPeers[address] = &PeerInfo{
			Address:  address,
			LastSeen: time.Now(),
			Source:   source,
		}
		DebugLog("Added peer to PEX cache: %s (source: %s)", address, source)
	}
}

// GetKnownPeers returns a list of known peers (excluding self and currently connected)
func (pex *PeerExchangeService) GetKnownPeers() []PeerInfo {
	pex.peerLock.RLock()
	defer pex.peerLock.RUnlock()

	peers := make([]PeerInfo, 0)

	// Get list of currently connected peers
	pex.server.PeerLock.Lock()
	connectedPeers := make(map[string]bool)
	for addr := range pex.server.Peers {
		connectedPeers[addr] = true
	}
	pex.server.PeerLock.Unlock()

	// Only include peers we're not currently connected to
	for addr, peer := range pex.knownPeers {
		if !connectedPeers[addr] {
			peers = append(peers, *peer)
		}
	}

	return peers
}

// periodicExchange periodically exchanges peer lists with connected peers
func (pex *PeerExchangeService) periodicExchange() {
	ticker := time.NewTicker(pex.exchangeInterval)
	defer ticker.Stop()

	// Do initial exchange after 30 seconds
	time.Sleep(30 * time.Second)
	pex.exchangePeerLists()

	for {
		select {
		case <-ticker.C:
			pex.exchangePeerLists()
		case <-pex.stopCh:
			return
		}
	}
}

// exchangePeerLists sends our peer list to all connected peers
func (pex *PeerExchangeService) exchangePeerLists() {
	if !pex.Enabled {
		return
	}

	// Get our list of known peers
	knownPeers := pex.GetKnownPeers()

	// Limit to 20 peers to avoid large messages
	if len(knownPeers) > 20 {
		knownPeers = knownPeers[:20]
	}

	if len(knownPeers) == 0 {
		return
	}

	// Create peer exchange message
	msg := Message{
		Payload: MessagePeerExchange{
			Peers: knownPeers,
		},
	}

	// Broadcast to all connected peers
	if err := pex.server.broadcast(&msg); err != nil {
		DebugLog("Failed to broadcast peer list: %v", err)
	} else {
		DebugLog("Exchanged peer list with %d known peers", len(knownPeers))
	}
}

// HandlePeerExchange processes a peer exchange message from another peer
func (pex *PeerExchangeService) HandlePeerExchange(from string, msg MessagePeerExchange) error {
	if !pex.Enabled {
		return nil
	}

	DebugLog("Received %d peers via PEX from %s", len(msg.Peers), from)

	newPeersFound := 0

	for _, peer := range msg.Peers {
		// Skip if it's our own address
		if peer.Address == pex.server.Transport.Addr() {
			continue
		}

		// Skip if we're already connected
		pex.server.PeerLock.Lock()
		_, alreadyConnected := pex.server.Peers[peer.Address]
		pex.server.PeerLock.Unlock()

		if alreadyConnected {
			continue
		}

		// Check if we already know about this peer
		pex.peerLock.RLock()
		_, alreadyKnown := pex.knownPeers[peer.Address]
		pex.peerLock.RUnlock()

		if alreadyKnown {
			continue
		}

		// Add to known peers
		pex.AddKnownPeer(peer.Address, "pex")
		newPeersFound++

		// Try to connect to the new peer
		go func(addr string) {
			log.Printf("Attempting to connect to peer learned via PEX: %s", addr)
			if err := pex.server.Transport.Dial(addr); err != nil {
				DebugLog("Failed to connect to PEX peer %s: %v", addr, err)
			} else {
				log.Printf("Successfully connected to peer %s learned via PEX", addr)
			}
		}(peer.Address)
	}

	if newPeersFound > 0 {
		log.Printf("Learned about %d new peers via PEX from %s", newPeersFound, from)
	}

	return nil
}

// periodicCleanup removes peers not seen in a while
func (pex *PeerExchangeService) periodicCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pex.cleanupOldPeers()
		case <-pex.stopCh:
			return
		}
	}
}

// cleanupOldPeers removes peers not seen in the last 30 minutes
func (pex *PeerExchangeService) cleanupOldPeers() {
	pex.peerLock.Lock()
	defer pex.peerLock.Unlock()

	cutoff := time.Now().Add(-30 * time.Minute)
	removed := 0

	for addr, peer := range pex.knownPeers {
		if peer.LastSeen.Before(cutoff) {
			delete(pex.knownPeers, addr)
			removed++
		}
	}

	if removed > 0 {
		DebugLog("Cleaned up %d stale peers from PEX cache", removed)
	}
}

// GetPeerCount returns the number of known peers
func (pex *PeerExchangeService) GetPeerCount() int {
	pex.peerLock.RLock()
	defer pex.peerLock.RUnlock()
	return len(pex.knownPeers)
}

// GetPeersBySource returns peers grouped by discovery source
func (pex *PeerExchangeService) GetPeersBySource() map[string]int {
	pex.peerLock.RLock()
	defer pex.peerLock.RUnlock()

	counts := make(map[string]int)
	for _, peer := range pex.knownPeers {
		counts[peer.Source]++
	}
	return counts
}

// ExportPeerList returns all known peers for debugging
func (pex *PeerExchangeService) ExportPeerList() []PeerInfo {
	pex.peerLock.RLock()
	defer pex.peerLock.RUnlock()

	peers := make([]PeerInfo, 0, len(pex.knownPeers))
	for _, peer := range pex.knownPeers {
		peers = append(peers, *peer)
	}
	return peers
}

// handleMessagePeerExchange is called by the server when a PEX message is received
func (s *FileServer) handleMessagePeerExchange(from string, msg MessagePeerExchange) error {
	if s.Pex != nil {
		return s.Pex.HandlePeerExchange(from, msg)
	}
	return nil
}

// RequestPeerList explicitly requests a peer list from a specific peer
func (pex *PeerExchangeService) RequestPeerList(peerAddr string) error {
	if !pex.Enabled {
		return fmt.Errorf("PEX is not enabled")
	}

	pex.server.PeerLock.Lock()
	peer, exists := pex.server.Peers[peerAddr]
	pex.server.PeerLock.Unlock()

	if !exists {
		return fmt.Errorf("peer %s not found", peerAddr)
	}

	// Send request message (we'll just send an empty PEX message as a request)
	msg := Message{
		Payload: MessagePeerExchange{
			Peers: []PeerInfo{},
		},
	}

	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(&msg); err != nil {
		return err
	}

	if err := peer.Send([]byte{p2p.IncomingMessage}); err != nil {
		return err
	}

	if err := peer.Send(buf.Bytes()); err != nil {
		return err
	}

	DebugLog("Requested peer list from %s", peerAddr)
	return nil
}
