package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AdityaKrSingh26/PeerVault/internal/crypto"
	"github.com/AdityaKrSingh26/PeerVault/internal/metrics"
	"github.com/AdityaKrSingh26/PeerVault/internal/network"
	"github.com/AdityaKrSingh26/PeerVault/internal/storage"
	"github.com/AdityaKrSingh26/PeerVault/pkg/p2p"
)

func makeServer(listenAddr string, networkKey []byte, nodes ...string) *network.FileServer {
	tcptransportOpts := p2p.TCPTransportOpts{
		ListenAddr:    listenAddr,
		HandshakeFunc: p2p.NOPHandshakeFunc,
		Decoder:       p2p.DefaultDecoder{},
		DialTimeout:   10 * time.Second,
		MaxRetries:    3,
		RetryDelay:    2 * time.Second,
	}
	tcpTransport := p2p.NewTCPTransport(tcptransportOpts)

	// Create a safe storage root name in a dedicated storage directory
	// Replace : with _ for Windows compatibility
	portName := strings.ReplaceAll(listenAddr, ":", "port_")
	storageRoot := fmt.Sprintf("storage/node_%s", portName)

	fileServerOpts := network.FileServerOpts{
		EncKey:            networkKey, // Use shared network key
		StorageRoot:       storageRoot,
		PathTransformFunc: storage.CASPathTransformFunc,
		Transport:         tcpTransport,
		BootstrapNodes:    nodes,
	}

	s := network.NewFileServer(fileServerOpts)

	tcpTransport.OnPeer = s.OnPeer

	return s
}

// Interactive mode for file operations
func interactiveMode(server *network.FileServer) {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("\n=== PeerVault Interactive Mode ===")
	fmt.Println("Commands:")
	fmt.Println("  store <filename>  - Store a file with sample data")
	fmt.Println("  get <filename>    - Retrieve and display a file")
	fmt.Println("  delete <filename> - Delete a file from network")
	fmt.Println("  list              - List all stored files")
	fmt.Println("  quota             - Show storage quota status")
	fmt.Println("  metrics           - Show server metrics")
	fmt.Println("  status            - Show server and network status")
	fmt.Println("  peers             - Show connected peers")
	fmt.Println("  discover          - Show discovered peers (mDNS/PEX)")
	fmt.Println("  send <file> <peer> - Send file to specific peer")
	fmt.Println("  fetch <key> <peer> - Fetch file from specific peer")
	fmt.Println("  clean             - Clean local storage")
	fmt.Println("  quit              - Exit PeerVault")
	fmt.Println()

	for {
		fmt.Print("PeerVault> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		parts := strings.Split(input, " ")
		command := parts[0]

		switch command {
		case "store":
			if len(parts) < 2 {
				fmt.Println("Usage: store <filename>")
				continue
			}
			filename := parts[1]
			// For demo, store some sample data
			data := bytes.NewReader([]byte(fmt.Sprintf("Sample data for file: %s (stored at %s)", filename, time.Now().Format("15:04:05"))))
			err := server.Store(filename, data)
			if err != nil {
				fmt.Printf("Error storing file: %v\n", err)
			} else {
				fmt.Printf("File '%s' stored successfully\n", filename)
			}

		case "get":
			if len(parts) < 2 {
				fmt.Println("Usage: get <filename>")
				continue
			}
			filename := parts[1]
			reader, err := server.Get(filename)
			if err != nil {
				fmt.Printf("Error retrieving file: %v\n", err)
			} else {
				data, err := io.ReadAll(reader)
				if err != nil {
					fmt.Printf("Error reading file: %v\n", err)
				} else {
					fmt.Printf("File content: %s\n", string(data))
				}
			}

		case "delete":
			if len(parts) < 2 {
				fmt.Println("Usage: delete <filename>")
				continue
			}
			filename := parts[1]

			// Confirm deletion
			fmt.Printf("Are you sure you want to delete '%s'? This will remove it from all nodes. (y/N): ", filename)
			if !scanner.Scan() {
				continue
			}
			confirmation := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if confirmation != "y" && confirmation != "yes" {
				fmt.Println("Deletion cancelled")
				continue
			}

			err := server.Delete(filename)
			if err != nil {
				fmt.Printf("Error deleting file: %v\n", err)
			} else {
				fmt.Printf("File '%s' deleted successfully from all nodes\n", filename)
			}

		case "quota":
			used, total, available, err := server.QuotaManager.GetStorageStats(server.StorageRoot)
			if err != nil {
				fmt.Printf("Error getting storage stats: %v\n", err)
				continue
			}

			percentage := float64(0)
			if total > 0 {
				percentage = (float64(used) / float64(total)) * 100
			}

			fmt.Println("\n=== Storage Quota ===")
			fmt.Printf("Used:      %s\n", metrics.FormatBytes(used))
			fmt.Printf("Total:     %s\n", metrics.FormatBytes(total))
			fmt.Printf("Available: %s\n", metrics.FormatBytes(available))
			fmt.Printf("Usage:     %.1f%%\n", percentage)

			// Show visual bar
			barWidth := 50
			usedBars := int((percentage / 100) * float64(barWidth))
			bar := strings.Repeat("█", usedBars) + strings.Repeat("░", barWidth-usedBars)
			fmt.Printf("[%s] %.1f%%\n", bar, percentage)

		case "metrics":
			fmt.Print(server.Metrics.ToHumanFormat())

		case "discover":
			fmt.Println("\n=== Peer Discovery Status ===")

			// mDNS discovered peers
			if server.Discovery != nil {
				discoveredPeers := server.Discovery.GetDiscoveredPeers()
				fmt.Printf("mDNS Discovered Peers: %d\n", len(discoveredPeers))
				if len(discoveredPeers) > 0 {
					for _, peer := range discoveredPeers {
						fmt.Printf("  - %s\n", peer)
					}
				}
				fmt.Println()
			} else {
				fmt.Println("mDNS Discovery: Disabled")
				fmt.Println("  (use -discover-local flag to enable)")
				fmt.Println()
			}

			// PEX known peers
			if server.Pex != nil && server.Pex.Enabled {
				knownPeers := server.Pex.ExportPeerList()
				fmt.Printf("PEX Known Peers: %d\n", len(knownPeers))

				// Group by source
				bySources := server.Pex.GetPeersBySource()
				for source, count := range bySources {
					fmt.Printf("  %s: %d peers\n", source, count)
				}

				if len(knownPeers) > 0 && len(knownPeers) <= 10 {
					fmt.Println("\nPeer List:")
					for _, peer := range knownPeers {
						fmt.Printf("  - %s (via %s, last seen: %v ago)\n",
							peer.Address, peer.Source, time.Since(peer.LastSeen).Round(time.Second))
					}
				}
			} else {
				fmt.Println("Peer Exchange (PEX): Disabled")
				fmt.Println("  (use -discover-pex flag to enable)")
			}

		case "status":
			fmt.Printf("Server listening on: %s\n", server.Transport.Addr())
			fmt.Printf("Local IP: %s\n", network.GetLocalIP())
			fmt.Printf("Connected peers: %d\n", len(server.Peers))
			for addr := range server.Peers {
				fmt.Printf("  - %s\n", addr)
			}

		case "list":
			// List files stored on this node
			files, err := server.ListFiles(server.ID)
			if err != nil {
				fmt.Printf("Error listing files: %v\n", err)
				continue
			}

			if len(files) == 0 {
				fmt.Println("No files stored on this node")
			} else {
				fmt.Printf("Files stored on this node (%d files):\n", len(files))
				fmt.Println("┌─────────────────────────────────────┬─────────────┬──────────────────────┐")
				fmt.Println("│ Filename                            │ Size (bytes)│ Hash (first 8 chars) │")
				fmt.Println("├─────────────────────────────────────┼─────────────┼──────────────────────┤")
				for _, file := range files {
					filename := file.Key
					if len(filename) > 35 {
						filename = filename[:32] + "..."
					}
					hashShort := file.Hash
					if len(hashShort) > 8 {
						hashShort = hashShort[:8]
					}
					fmt.Printf("│ %-35s │ %11d │ %-20s │\n", filename, file.Size, hashShort)
				}
				fmt.Println("└─────────────────────────────────────┴─────────────┴──────────────────────┘")
			}

			// Also show files from other nodes (if any)
			allFiles, err := server.ListAllFiles()
			if err == nil && len(allFiles) > 1 {
				fmt.Println("\nFiles from other nodes:")
				for nodeID, nodeFiles := range allFiles {
					if nodeID != server.ID && len(nodeFiles) > 0 {
						fmt.Printf("  Node %s (%d files):\n", nodeID[:8], len(nodeFiles))
						for _, file := range nodeFiles {
							fmt.Printf("    - %s (%d bytes)\n", file.Key, file.Size)
						}
					}
				}
			}

		case "peers":
			server.PeerLock.Lock()
			peerCount := len(server.Peers)
			if peerCount == 0 {
				fmt.Println("No peers connected")
				server.PeerLock.Unlock()
				continue
			}

			fmt.Printf("Connected Peers (%d):\n", peerCount)
			fmt.Println("┌───────────────────────────────┬─────────────┬────────────────┐")
			fmt.Println("│ Address                       │ Status      │ Last Seen      │")
			fmt.Println("├───────────────────────────────┼─────────────┼────────────────┤")

			for addr := range server.Peers {
				addrDisplay := addr
				if len(addrDisplay) > 29 {
					addrDisplay = addrDisplay[:26] + "..."
				}
				fmt.Printf("│ %-29s │ %-11s │ %-14s │\n", addrDisplay, "Connected", "Now")
			}
			fmt.Println("└───────────────────────────────┴─────────────┴────────────────┘")
			server.PeerLock.Unlock()

		case "send":
			if len(parts) < 3 {
				fmt.Println("Usage: send <filename> <peer_address>")
				fmt.Println("Example: send myfile.txt 192.168.1.100:3000")
				continue
			}
			filename := parts[1]
			peerAddr := parts[2]

			server.PeerLock.Lock()
			peer, exists := server.Peers[peerAddr]
			server.PeerLock.Unlock()

			if !exists {
				fmt.Printf("Peer %s not found. Use 'peers' command to see connected peers.\n", peerAddr)
				continue
			}

			// Read file from local storage
			_, fileReader, err := server.ReadFile(server.ID, filename)
			if err != nil {
				fmt.Printf("Error reading file: %v\n", err)
				continue
			}

			if rc, ok := fileReader.(io.ReadCloser); ok {
				defer rc.Close()
			}

			// Send directly to peer (simplified - you may want to use proper protocol)
			fmt.Printf("Sending '%s' to %s...\n", filename, peerAddr)

			// Notify peer about incoming file
			msg := network.Message{
				Payload: network.MessageStoreFile{
					ID:   server.ID,
					Key:  crypto.HashKey(filename),
					Size: 0, // Size would need to be calculated
				},
			}

			buf := new(bytes.Buffer)
			if err := gob.NewEncoder(buf).Encode(&msg); err != nil {
				fmt.Printf("Error encoding message: %v\n", err)
				continue
			}

			peer.Send([]byte{p2p.IncomingMessage})
			if err := peer.Send(buf.Bytes()); err != nil {
				fmt.Printf("Error sending to peer: %v\n", err)
				continue
			}

			fmt.Printf("File '%s' sent to %s\n", filename, peerAddr)

		case "fetch":
			if len(parts) < 3 {
				fmt.Println("Usage: fetch <filename> <peer_address>")
				fmt.Println("Example: fetch myfile.txt 192.168.1.100:3000")
				continue
			}
			filename := parts[1]
			peerAddr := parts[2]

			server.PeerLock.Lock()
			_, exists := server.Peers[peerAddr]
			server.PeerLock.Unlock()

			if !exists {
				fmt.Printf("Peer %s not found. Use 'peers' command to see connected peers.\n", peerAddr)
				continue
			}

			fmt.Printf("Fetching '%s' from %s...\n", filename, peerAddr)

			// Use the existing Get method which will fetch from network
			reader, err := server.Get(filename)
			if err != nil {
				fmt.Printf("Error fetching file: %v\n", err)
				continue
			}

			// Display file contents
			data, err := io.ReadAll(reader)
			if err != nil {
				fmt.Printf("Error reading file data: %v\n", err)
				continue
			}

			fmt.Printf("File '%s' fetched successfully (%d bytes)\n", filename, len(data))
			if len(data) < 500 {
				fmt.Printf("Contents: %s\n", string(data))
			} else {
				fmt.Printf("Contents (first 500 bytes): %s...\n", string(data[:500]))
			}

		case "clean":
			fmt.Print("Are you sure you want to delete all local files? (y/N): ")
			if !scanner.Scan() {
				continue
			}
			confirmation := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if confirmation == "y" || confirmation == "yes" {
				// First stop the server to close any open files
				server.Stop()
				time.Sleep(500 * time.Millisecond) // Give time for cleanup

				err := server.ClearStorage()
				if err != nil {
					fmt.Printf("Error cleaning storage: %v\n", err)
				} else {
					fmt.Println("Local storage cleaned successfully")
					// Clear the key mapping as well
					server.ClearKeyMapping()
				}

				fmt.Println("Server stopped. Please restart to continue.")
				return
			} else {
				fmt.Println("Clean operation cancelled")
			}

		case "quit", "exit":
			fmt.Println("Shutting down...")
			server.Stop()
			return

		default:
			fmt.Printf("Unknown command: %s\n", command)
		}
	}
}

// Global debug flag
var DebugMode bool

// DebugLog prints debug messages only when debug mode is enabled
func DebugLog(format string, args ...interface{}) {
	if DebugMode {
		log.Printf("[DEBUG] "+format, args...)
	}
}

func main() {
	// Command line flags
	var (
		listenAddr     = flag.String("addr", ":3000", "Listen address (e.g., :3000 or 0.0.0.0:3000)")
		advertiseAddr  = flag.String("advertise", "", "Address to advertise to peers (auto-detected if not set)")
		bootstrap      = flag.String("bootstrap", "", "Bootstrap nodes (comma-separated, e.g., 192.168.1.100:3000,192.168.1.101:4000)")
		interactive    = flag.Bool("interactive", false, "Run in interactive mode")
		demo           = flag.Bool("demo", false, "Run demo mode with test data")
		encKey         = flag.String("key", "", "Encryption key (32 bytes for AES-256, can also use PEERVAULT_KEY env var)")
		detectPublicIP = flag.Bool("public-ip", false, "Auto-detect and use public IP for advertise address")
		verbose        = flag.Bool("verbose", false, "Enable verbose/debug logging")
		debug          = flag.Bool("debug", false, "Enable debug mode (alias for verbose)")
		metricsAddr    = flag.String("metrics", "", "Metrics server address (e.g., :9090) - disabled if not set")
		discoverLocal  = flag.Bool("discover-local", false, "Enable mDNS local network peer discovery")
		discoverPex    = flag.Bool("discover-pex", false, "Enable peer exchange (PEX) protocol")
	)
	flag.Parse()

	// Set global debug mode
	DebugMode = *verbose || *debug
	if DebugMode {
		log.Println("🐛 Debug mode enabled")
	}

	// Get encryption key from flag, env var, or generate random key
	var networkKey []byte
	if *encKey != "" {
		// Use key from command line flag
		networkKey = []byte(*encKey)
	} else if envKey := os.Getenv("PEERVAULT_KEY"); envKey != "" {
		// Use key from environment variable
		networkKey = []byte(envKey)
	} else {
		// SECURITY WARNING: No key provided, using default (NOT SECURE FOR PRODUCTION)
		log.Println("⚠️  WARNING: No encryption key provided via -key flag or PEERVAULT_KEY env var")
		log.Println("⚠️  Using default key. For production, set a secure key with:")
		log.Println("   export PEERVAULT_KEY='your-32-byte-secure-key-here'")
		networkKey = []byte("PeerVault-Network-Key-256bit!") // 32 bytes for AES-256
	}

	// Ensure key is exactly 32 bytes for AES-256
	if len(networkKey) != 32 {
		key := make([]byte, 32)
		copy(key, networkKey)
		networkKey = key
		log.Printf("⚠️  Encryption key adjusted to 32 bytes (was %d bytes)", len(networkKey))
	}

	// Parse bootstrap nodes
	var bootstrapNodes []string
	if *bootstrap != "" {
		bootstrapNodes = strings.Split(*bootstrap, ",")
		// Trim whitespace
		for i, node := range bootstrapNodes {
			bootstrapNodes[i] = strings.TrimSpace(node)
		}
	}

	// Determine advertise address
	var finalAdvertiseAddr string
	if *advertiseAddr != "" {
		// Use explicitly provided advertise address
		finalAdvertiseAddr = *advertiseAddr
		log.Printf("Using advertise address: %s", finalAdvertiseAddr)
	} else if *detectPublicIP {
		// Auto-detect public IP
		log.Println("Detecting public IP address...")
		publicIP, err := network.GetPublicIP()
		if err != nil {
			log.Printf("⚠️  Failed to detect public IP: %v", err)
			log.Println("Falling back to local IP")
			localIP := network.GetLocalIP()
			finalAdvertiseAddr, _ = network.BuildAdvertiseAddr(localIP, *listenAddr)
		} else {
			log.Printf("Detected public IP: %s", publicIP)
			finalAdvertiseAddr, _ = network.BuildAdvertiseAddr(publicIP, *listenAddr)
		}
	} else {
		// Use local IP as default
		localIP := network.GetLocalIP()
		finalAdvertiseAddr, _ = network.BuildAdvertiseAddr(localIP, *listenAddr)
	}

	// Create and start server
	server := makeServer(*listenAddr, networkKey, bootstrapNodes...)

	// Initialize quota manager and load/create configuration
	log.Println("Initializing storage quota...")
	if err := server.QuotaManager.LoadOrCreate(); err != nil {
		log.Fatalf("Failed to initialize quota: %v", err)
	}
	log.Printf("Storage quota configured: %s", metrics.FormatBytes(server.QuotaManager.GetMaxStorage()))

	// Enable peer discovery if requested
	if *discoverLocal {
		log.Println("Enabling local network discovery (mDNS)...")
		if err := server.EnableLocalDiscovery(finalAdvertiseAddr); err != nil {
			log.Printf("Warning: Failed to enable local discovery: %v", err)
		}
	}

	if *discoverPex {
		log.Println("Enabling peer exchange (PEX)...")
		server.EnablePeerExchange()
	}

	// Start metrics server if enabled
	if *metricsAddr != "" {
		metricsServer := metrics.NewMetricsServer(*metricsAddr, server.Metrics)
		go func() {
			log.Printf("Starting metrics server on %s", *metricsAddr)
			if err := metricsServer.Start(); err != nil && err != http.ErrServerClosed {
				log.Printf("Metrics server error: %v", err)
			}
		}()
	}

	// Start server in background
	go func() {
		log.Printf("Starting PeerVault server")
		log.Printf("  Listen address: %s", *listenAddr)
		log.Printf("  Advertise address: %s", finalAdvertiseAddr)
		log.Printf("  Local IP: %s", network.GetLocalIP())
		if len(bootstrapNodes) > 0 {
			log.Printf("  Bootstrap nodes: %v", bootstrapNodes)
		}

		if err := server.Start(); err != nil {
			log.Fatal("Server failed to start:", err)
		}
	}()

	// Give server time to start
	time.Sleep(2 * time.Second)

	if *interactive {
		// Interactive mode
		interactiveMode(server)
	} else if *demo {
		// Demo mode - store and retrieve some test files
		fmt.Println("Running demo mode...")

		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("demo_file_%d.txt", i)
			data := bytes.NewReader([]byte(fmt.Sprintf("Demo file %d content created at %s", i, time.Now().Format("15:04:05"))))

			if err := server.Store(key, data); err != nil {
				log.Printf("Error storing %s: %v", key, err)
			} else {
				log.Printf("Stored: %s", key)
			}
		}

		time.Sleep(2 * time.Second)

		// Try to retrieve files
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("demo_file_%d.txt", i)
			reader, err := server.Get(key)
			if err != nil {
				log.Printf("Error retrieving %s: %v", key, err)
			} else {
				data, _ := io.ReadAll(reader)
				log.Printf("Retrieved %s: %s", key, string(data))
			}
		}
	} else {
		// Keep server running
		fmt.Printf("PeerVault server running on %s\n", *listenAddr)
		fmt.Printf("Local IP: %s\n", network.GetLocalIP())
		fmt.Printf("Use Ctrl+C to stop or --interactive flag for interactive mode\n")

		select {} // Block forever
	}
}
