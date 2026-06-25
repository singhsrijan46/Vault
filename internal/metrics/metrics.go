package metrics

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics tracks various server statistics
type Metrics struct {
	// Counters
	filesStored    int64
	filesRetrieved int64
	filesDeleted   int64
	bytesSent      int64
	bytesReceived  int64
	errorsTotal    int64

	// Gauges (current values)
	peersConnected  int64
	peersDiscovered int64 // Peers discovered via mDNS/PEX
	storageUsed     int64
	storageTotal    int64

	// Timing
	startTime      time.Time
	lastUpdateTime time.Time

	mu sync.RWMutex
}

// NewMetrics creates a new metrics collector
func NewMetrics() *Metrics {
	return &Metrics{
		startTime:      time.Now(),
		lastUpdateTime: time.Now(),
	}
}

// File operation metrics
func (m *Metrics) IncFilesStored() {
	atomic.AddInt64(&m.filesStored, 1)
	m.updateTime()
}

func (m *Metrics) IncFilesRetrieved() {
	atomic.AddInt64(&m.filesRetrieved, 1)
	m.updateTime()
}

func (m *Metrics) IncFilesDeleted() {
	atomic.AddInt64(&m.filesDeleted, 1)
	m.updateTime()
}

// Network transfer metrics
func (m *Metrics) AddBytesSent(bytes int64) {
	atomic.AddInt64(&m.bytesSent, bytes)
	m.updateTime()
}

func (m *Metrics) AddBytesReceived(bytes int64) {
	atomic.AddInt64(&m.bytesReceived, bytes)
	m.updateTime()
}

// Error metrics
func (m *Metrics) IncErrors() {
	atomic.AddInt64(&m.errorsTotal, 1)
	m.updateTime()
}

// Gauge metrics (set values)
func (m *Metrics) SetPeersConnected(count int) {
	atomic.StoreInt64(&m.peersConnected, int64(count))
	m.updateTime()
}

func (m *Metrics) SetPeersDiscovered(count int) {
	atomic.StoreInt64(&m.peersDiscovered, int64(count))
	m.updateTime()
}

func (m *Metrics) SetStorageUsed(bytes int64) {
	atomic.StoreInt64(&m.storageUsed, bytes)
	m.updateTime()
}

func (m *Metrics) SetStorageTotal(bytes int64) {
	atomic.StoreInt64(&m.storageTotal, bytes)
	m.updateTime()
}

// Update last activity time
func (m *Metrics) updateTime() {
	m.mu.Lock()
	m.lastUpdateTime = time.Now()
	m.mu.Unlock()
}

// GetUptime returns the server uptime
func (m *Metrics) GetUptime() time.Duration {
	return time.Since(m.startTime)
}

// ToPrometheusFormat exports metrics in Prometheus text format
func (m *Metrics) ToPrometheusFormat() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	uptime := time.Since(m.startTime).Seconds()

	return fmt.Sprintf(`# HELP peervault_files_stored_total Total number of files stored
# TYPE peervault_files_stored_total counter
peervault_files_stored_total %d

# HELP peervault_files_retrieved_total Total number of files retrieved
# TYPE peervault_files_retrieved_total counter
peervault_files_retrieved_total %d

# HELP peervault_files_deleted_total Total number of files deleted
# TYPE peervault_files_deleted_total counter
peervault_files_deleted_total %d

# HELP peervault_bytes_sent_total Total bytes sent to peers
# TYPE peervault_bytes_sent_total counter
peervault_bytes_sent_total %d

# HELP peervault_bytes_received_total Total bytes received from peers
# TYPE peervault_bytes_received_total counter
peervault_bytes_received_total %d

# HELP peervault_errors_total Total number of errors
# TYPE peervault_errors_total counter
peervault_errors_total %d

# HELP peervault_peers_connected Current number of connected peers
# TYPE peervault_peers_connected gauge
peervault_peers_connected %d

# HELP peervault_peers_discovered Peers discovered via mDNS/PEX
# TYPE peervault_peers_discovered gauge
peervault_peers_discovered %d

# HELP peervault_storage_used_bytes Current storage used in bytes
# TYPE peervault_storage_used_bytes gauge
peervault_storage_used_bytes %d

# HELP peervault_storage_total_bytes Total storage capacity in bytes
# TYPE peervault_storage_total_bytes gauge
peervault_storage_total_bytes %d

# HELP peervault_storage_utilization Storage utilization percentage (0-100)
# TYPE peervault_storage_utilization gauge
peervault_storage_utilization %.2f

# HELP peervault_uptime_seconds Server uptime in seconds
# TYPE peervault_uptime_seconds gauge
peervault_uptime_seconds %.2f
`,
		atomic.LoadInt64(&m.filesStored),
		atomic.LoadInt64(&m.filesRetrieved),
		atomic.LoadInt64(&m.filesDeleted),
		atomic.LoadInt64(&m.bytesSent),
		atomic.LoadInt64(&m.bytesReceived),
		atomic.LoadInt64(&m.errorsTotal),
		atomic.LoadInt64(&m.peersConnected),
		atomic.LoadInt64(&m.peersDiscovered),
		atomic.LoadInt64(&m.storageUsed),
		atomic.LoadInt64(&m.storageTotal),
		m.getStorageUtilization(),
		uptime,
	)
}

// ToJSONFormat exports metrics in JSON format
func (m *Metrics) ToJSONFormat() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	uptime := time.Since(m.startTime).Seconds()

	return fmt.Sprintf(`{
  "files": {
    "stored": %d,
    "retrieved": %d,
    "deleted": %d
  },
  "network": {
    "bytes_sent": %d,
    "bytes_received": %d,
    "peers_connected": %d,
    "peers_discovered": %d
  },
  "storage": {
    "used_bytes": %d,
    "total_bytes": %d,
    "utilization_percent": %.2f
  },
  "errors": {
    "total": %d
  },
  "system": {
    "uptime_seconds": %.2f,
    "start_time": "%s",
    "last_update": "%s"
  }
}`,
		atomic.LoadInt64(&m.filesStored),
		atomic.LoadInt64(&m.filesRetrieved),
		atomic.LoadInt64(&m.filesDeleted),
		atomic.LoadInt64(&m.bytesSent),
		atomic.LoadInt64(&m.bytesReceived),
		atomic.LoadInt64(&m.peersConnected),
		atomic.LoadInt64(&m.peersDiscovered),
		atomic.LoadInt64(&m.storageUsed),
		atomic.LoadInt64(&m.storageTotal),
		m.getStorageUtilization(),
		atomic.LoadInt64(&m.errorsTotal),
		uptime,
		m.startTime.Format(time.RFC3339),
		m.lastUpdateTime.Format(time.RFC3339),
	)
}

// ToHumanFormat exports metrics in human-readable format
func (m *Metrics) ToHumanFormat() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	uptime := m.GetUptime()
	days := int(uptime.Hours() / 24)
	hours := int(uptime.Hours()) % 24
	minutes := int(uptime.Minutes()) % 60

	uptimeStr := fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	if days == 0 {
		uptimeStr = fmt.Sprintf("%dh %dm", hours, minutes)
		if hours == 0 {
			uptimeStr = fmt.Sprintf("%dm", minutes)
		}
	}

	return fmt.Sprintf(`=== PeerVault Metrics ===

File Operations:
  Stored:     %d
  Retrieved:  %d
  Deleted:    %d

Network:
  Bytes Sent:     %s
  Bytes Received: %s
  Peers Connected: %d

Storage:
  Used:        %s
  Total:       %s
  Utilization: %.1f%%

System:
  Errors:  %d
  Uptime:  %s
  Started: %s
`,
		atomic.LoadInt64(&m.filesStored),
		atomic.LoadInt64(&m.filesRetrieved),
		atomic.LoadInt64(&m.filesDeleted),
		FormatBytes(atomic.LoadInt64(&m.bytesSent)),
		FormatBytes(atomic.LoadInt64(&m.bytesReceived)),
		atomic.LoadInt64(&m.peersConnected),
		FormatBytes(atomic.LoadInt64(&m.storageUsed)),
		FormatBytes(atomic.LoadInt64(&m.storageTotal)),
		m.getStorageUtilization(),
		atomic.LoadInt64(&m.errorsTotal),
		uptimeStr,
		m.startTime.Format("2006-01-02 15:04:05"),
	)
}

// getStorageUtilization calculates storage utilization percentage
func (m *Metrics) getStorageUtilization() float64 {
	total := atomic.LoadInt64(&m.storageTotal)
	if total == 0 {
		return 0.0
	}
	used := atomic.LoadInt64(&m.storageUsed)
	return (float64(used) / float64(total)) * 100
}

// Update storage metrics from quota manager
func (m *Metrics) UpdateStorageMetrics(used, total int64) {
	m.SetStorageUsed(used)
	m.SetStorageTotal(total)
}

// GetSummary returns a brief summary of key metrics
func (m *Metrics) GetSummary() string {
	return fmt.Sprintf("Files: %d stored, %d retrieved | Peers: %d | Storage: %.1f%% used",
		atomic.LoadInt64(&m.filesStored),
		atomic.LoadInt64(&m.filesRetrieved),
		atomic.LoadInt64(&m.peersConnected),
		m.getStorageUtilization(),
	)
}
