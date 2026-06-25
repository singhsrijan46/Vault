package network

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/AdityaKrSingh26/PeerVault/internal/crypto"
	"github.com/AdityaKrSingh26/PeerVault/internal/metrics"
	"github.com/AdityaKrSingh26/PeerVault/internal/quota"
	"github.com/AdityaKrSingh26/PeerVault/internal/storage"
	"github.com/AdityaKrSingh26/PeerVault/pkg/p2p"
)

// configuration options
type FileServerOpts struct {
	ID                string
	EncKey            []byte
	StorageRoot       string
	PathTransformFunc storage.PathTransformFunc
	Transport         p2p.Transport
	BootstrapNodes    []string
}

// Manages file storage, peer connections, and network communication.
type FileServer struct {
	FileServerOpts

	PeerLock sync.Mutex
	Peers    map[string]p2p.Peer

	store        *storage.Store
	QuotaManager *quota.QuotaManager
	GC           *storage.GarbageCollector
	Metrics      *metrics.Metrics
	Discovery    *DiscoveryService
	Pex          *PeerExchangeService
	quitch       chan struct{}
}

// Initializes a new "FileServer" instance.
func NewFileServer(opts FileServerOpts) *FileServer {
	storeOpts := storage.StoreOpts{
		Root:              opts.StorageRoot,
		PathTransformFunc: opts.PathTransformFunc,
	}

	if len(opts.ID) == 0 {
		opts.ID = crypto.GenerateID()
	}

	store := storage.NewStore(storeOpts)
	quotaManager := quota.NewQuotaManager(opts.StorageRoot)
	gc := storage.NewGarbageCollector(store, opts.ID)
	metricsObj := metrics.NewMetrics()

	server := &FileServer{
		FileServerOpts: opts,
		store:          store,
		QuotaManager:   quotaManager,
		GC:             gc,
		Metrics:        metricsObj,
		quitch:         make(chan struct{}),
		Peers:          make(map[string]p2p.Peer),
	}

	server.Pex = NewPeerExchangeService(server)
	return server
}

// Sends a message to all connected peers.
func (s *FileServer) broadcast(msg *Message) error {
	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(msg); err != nil {
		return err
	}

	for _, peer := range s.Peers {
		peer.Send([]byte{p2p.IncomingMessage})
		if err := peer.Send(buf.Bytes()); err != nil {
			return err
		}
	}

	return nil
}

// Generic message wrapper
type Message struct {
	Payload any
}

// Notifies peers about a file being stored
type MessageStoreFile struct {
	ID   string
	Key  string
	Size int64
}

// Requests a file from peers
type MessageGetFile struct {
	ID  string
	Key string
}

// Retrieves a file from the local store or fetches it from the network.
func (s *FileServer) Get(key string) (io.Reader, error) {

	// Checks if the file exists locally.
	if s.store.Has(s.ID, key) {
		fmt.Printf("[%s] serving file (%s) from local disk\n", s.Transport.Addr(), key)
		_, r, err := s.store.Read(s.ID, key)
		return r, err
	}

	fmt.Printf("[%s] dont have file (%s) locally, fetching from network...\n", s.Transport.Addr(), key)

	// If not, broadcasts a MessageGetFile request to peers.
	msg := Message{
		Payload: MessageGetFile{
			ID:  s.ID,
			Key: crypto.HashKey(key),
		},
	}
	if err := s.broadcast(&msg); err != nil {
		return nil, err
	}

	time.Sleep(time.Millisecond * 500)

	// Receives the file from a peer and stores it locally.
	for _, peer := range s.Peers {
		// First read the file size so we can limit the amount of bytes that we read
		// from the connection, so it will not keep hanging.
		var fileSize int64
		binary.Read(peer, binary.LittleEndian, &fileSize)

		// storing the file locally
		n, err := s.store.WriteDecrypt(s.EncKey, s.ID, key, io.LimitReader(peer, fileSize))
		if err != nil {
			return nil, err
		}

		fmt.Printf("[%s] received (%d) bytes over the network from (%s)", s.Transport.Addr(), n, peer.RemoteAddr())

		peer.CloseStream()
	}

	_, r, err := s.store.Read(s.ID, key)
	return r, err
}

// Stores a file locally and notifies peers.
func (s *FileServer) Store(key string, r io.Reader) error {
	var (
		fileBuffer = new(bytes.Buffer)
		tee        = io.TeeReader(r, fileBuffer)
	)

	size, err := s.store.Write(s.ID, key, tee)
	if err != nil {
		return err
	}

	msg := Message{
		Payload: MessageStoreFile{
			ID:   s.ID,
			Key:  crypto.HashKey(key),
			Size: size + 16,
		},
	}

	if err := s.broadcast(&msg); err != nil {
		return err
	}

	time.Sleep(time.Millisecond * 5)

	peers := []io.Writer{}
	for _, peer := range s.Peers {
		peers = append(peers, peer)
	}
	mw := io.MultiWriter(peers...)
	mw.Write([]byte{p2p.IncomingStream})
	n, err := crypto.CopyEncrypt(s.EncKey, fileBuffer, mw)
	if err != nil {
		return err
	}

	fmt.Printf("[%s] received and written (%d) bytes to disk\n", s.Transport.Addr(), n)

	return nil
}

func (s *FileServer) Stop() {
	close(s.quitch)
}

// Handles new peer connections.
func (s *FileServer) OnPeer(p p2p.Peer) error {
	s.PeerLock.Lock()
	defer s.PeerLock.Unlock()

	// Adds the peer to the peers map.
	s.Peers[p.RemoteAddr().String()] = p

	log.Printf("connected with remote %s", p.RemoteAddr())

	return nil
}

// Main event loop for handling incoming messages.
func (s *FileServer) loop() {
	defer func() {
		log.Println("file server stopped due to error or user quit action")
		s.Transport.Close()
	}()

	for {
		select {
		case rpc := <-s.Transport.Consume():
			var msg Message
			if err := gob.NewDecoder(bytes.NewReader(rpc.Payload)).Decode(&msg); err != nil {
				log.Println("decoding error: ", err)
			}
			if err := s.handleMessage(rpc.From, &msg); err != nil {
				log.Println("handle message error: ", err)
			}

		case <-s.quitch:
			return
		}
	}
}

// Processes incoming messages.
func (s *FileServer) handleMessage(from string, msg *Message) error {
	switch v := msg.Payload.(type) {
	case MessageStoreFile:
		return s.handleMessageStoreFile(from, v)
	case MessageGetFile:
		return s.handleMessageGetFile(from, v)
	}

	return nil
}

func (s *FileServer) handleMessageGetFile(from string, msg MessageGetFile) error {
	if !s.store.Has(msg.ID, msg.Key) {
		return fmt.Errorf("[%s] need to serve file (%s) but it does not exist on disk", s.Transport.Addr(), msg.Key)
	}

	fmt.Printf("[%s] serving file (%s) over the network\n", s.Transport.Addr(), msg.Key)

	fileSize, r, err := s.store.Read(msg.ID, msg.Key)
	if err != nil {
		return err
	}

	if rc, ok := r.(io.ReadCloser); ok {
		fmt.Println("closing readCloser")
		defer rc.Close()
	}

	peer, ok := s.Peers[from]
	if !ok {
		return fmt.Errorf("peer %s not in map", from)
	}

	// First send the "incomingStream" byte to the peer and then we can send
	// the file size as an int64.
	peer.Send([]byte{p2p.IncomingStream})
	binary.Write(peer, binary.LittleEndian, fileSize)
	n, err := io.Copy(peer, r)
	if err != nil {
		return err
	}

	fmt.Printf("[%s] written (%d) bytes over the network to %s\n", s.Transport.Addr(), n, from)

	return nil
}

func (s *FileServer) handleMessageStoreFile(from string, msg MessageStoreFile) error {
	peer, ok := s.Peers[from]
	if !ok {
		return fmt.Errorf("peer (%s) could not be found in the peer list", from)
	}

	n, err := s.store.Write(msg.ID, msg.Key, io.LimitReader(peer, msg.Size))
	if err != nil {
		return err
	}

	fmt.Printf("[%s] written %d bytes to disk\n", s.Transport.Addr(), n)

	peer.CloseStream()

	return nil
}

func (s *FileServer) bootstrapNetwork() error {
	for _, addr := range s.BootstrapNodes {
		if len(addr) == 0 {
			continue
		}

		go func(addr string) {
			fmt.Printf("[%s] attemping to connect with remote %s\n", s.Transport.Addr(), addr)
			if err := s.Transport.Dial(addr); err != nil {
				log.Println("dial error: ", err)
			}
		}(addr)
	}

	return nil
}

func (s *FileServer) Start() error {
	fmt.Printf("[%s] starting fileserver...\n", s.Transport.Addr())

	if err := s.Transport.ListenAndAccept(); err != nil {
		return err
	}

	s.bootstrapNetwork()

	s.loop()

	return nil
}

func init() {
	gob.Register(MessageStoreFile{})
	gob.Register(MessageGetFile{})
}

// Delete removes a file from local storage and broadcasts deletion to peers

// Delete removes a file
func (s *FileServer) Delete(key string) error {
	if !s.store.Has(s.ID, key) {
		return fmt.Errorf("file not found")
	}
	return s.store.Delete(s.ID, key)
}

// EnableLocalDiscovery enables mDNS discovery
func (s *FileServer) EnableLocalDiscovery(advertiseAddr string) error {
	s.Discovery = NewDiscoveryService("peervault", 3000, advertiseAddr)
	s.Discovery.SetPeerFoundCallback(func(peerAddr string) error {
		return s.Transport.Dial(peerAddr)
	})
	return s.Discovery.Start()
}

// EnablePeerExchange enables PEX
func (s *FileServer) EnablePeerExchange() {
	if s.Pex != nil {
		s.Pex.Start()
	}
}

func init() {
	gob.Register(MessageStoreFile{})
	gob.Register(MessageGetFile{})
}

// Public store accessors
func (s *FileServer) ListFiles(id string) ([]storage.FileInfo, error) {
	return s.store.List(id)
}

func (s *FileServer) ListAllFiles() (map[string][]storage.FileInfo, error) {
	return s.store.ListAll()
}

func (s *FileServer) ReadFile(id, key string) (int64, io.Reader, error) {
	return s.store.Read(id, key)
}

func (s *FileServer) ClearStorage() error {
	return s.store.Clear()
}

func (s *FileServer) ClearKeyMapping() {
	s.store.ClearKeyMap()
}
