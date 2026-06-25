package metrics

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

// MetricsServer serves metrics over HTTP
type MetricsServer struct {
	addr    string
	metrics *Metrics
	server  *http.Server
}

// NewMetricsServer creates a new metrics HTTP server
func NewMetricsServer(addr string, metrics *Metrics) *MetricsServer {
	return &MetricsServer{
		addr:    addr,
		metrics: metrics,
	}
}

// Start begins serving metrics over HTTP
func (ms *MetricsServer) Start() error {
	mux := http.NewServeMux()

	// Prometheus format endpoint
	mux.HandleFunc("/metrics", ms.handleMetrics)

	// JSON format endpoint
	mux.HandleFunc("/metrics/json", ms.handleMetricsJSON)

	// Human-readable format endpoint
	mux.HandleFunc("/metrics/human", ms.handleMetricsHuman)

	// Health check endpoint
	mux.HandleFunc("/health", ms.handleHealth)

	// Root endpoint with documentation
	mux.HandleFunc("/", ms.handleRoot)

	ms.server = &http.Server{
		Addr:    ms.addr,
		Handler: mux,
	}

	log.Printf("Starting metrics server on %s", ms.addr)
	return ms.server.ListenAndServe()
}

// Stop gracefully shuts down the metrics server
func (ms *MetricsServer) Stop() error {
	if ms.server != nil {
		return ms.server.Close()
	}
	return nil
}

// handleMetrics serves metrics in Prometheus format
func (ms *MetricsServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ms.metrics.ToPrometheusFormat())
}

// handleMetricsJSON serves metrics in JSON format
func (ms *MetricsServer) handleMetricsJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ms.metrics.ToJSONFormat())
}

// handleMetricsHuman serves metrics in human-readable format
func (ms *MetricsServer) handleMetricsHuman(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ms.metrics.ToHumanFormat())
}

// handleHealth serves a simple health check endpoint
func (ms *MetricsServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"healthy","uptime_seconds":%.2f}`, ms.metrics.GetUptime().Seconds())
}

// handleRoot serves documentation about available endpoints
func (ms *MetricsServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)

	html := `<!DOCTYPE html>
<html>
<head>
    <title>PeerVault Metrics</title>
    <style>
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            max-width: 900px;
            margin: 40px auto;
            padding: 20px;
            background: #f5f5f5;
        }
        .container {
            background: white;
            padding: 30px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        h1 {
            color: #333;
            border-bottom: 3px solid #4a90e2;
            padding-bottom: 10px;
        }
        h2 {
            color: #555;
            margin-top: 30px;
        }
        .endpoint {
            background: #f8f9fa;
            padding: 15px;
            margin: 10px 0;
            border-left: 4px solid #4a90e2;
            border-radius: 4px;
        }
        .endpoint a {
            color: #4a90e2;
            text-decoration: none;
            font-weight: bold;
            font-size: 16px;
        }
        .endpoint a:hover {
            text-decoration: underline;
        }
        .endpoint p {
            margin: 5px 0 0 0;
            color: #666;
        }
        .metrics-preview {
            background: #f8f9fa;
            padding: 15px;
            margin: 20px 0;
            border-radius: 4px;
            font-family: monospace;
            white-space: pre-wrap;
            font-size: 12px;
        }
        .footer {
            margin-top: 40px;
            text-align: center;
            color: #999;
            font-size: 14px;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>🔧 PeerVault Metrics Server</h1>
        <p>Welcome to the PeerVault metrics interface. This server exposes various metrics about the PeerVault node.</p>

        <h2>Available Endpoints:</h2>

        <div class="endpoint">
            <a href="/metrics">/metrics</a>
            <p>Metrics in Prometheus format (suitable for scraping with Prometheus)</p>
        </div>

        <div class="endpoint">
            <a href="/metrics/json">/metrics/json</a>
            <p>Metrics in JSON format (suitable for programmatic access)</p>
        </div>

        <div class="endpoint">
            <a href="/metrics/human">/metrics/human</a>
            <p>Metrics in human-readable format</p>
        </div>

        <div class="endpoint">
            <a href="/health">/health</a>
            <p>Health check endpoint</p>
        </div>

        <h2>Quick Preview:</h2>
        <div class="metrics-preview">` + escapeHTML(ms.metrics.GetSummary()) + `</div>

        <div class="footer">
            <p>PeerVault - Distributed P2P File Storage</p>
        </div>
    </div>
</body>
</html>`

	fmt.Fprint(w, html)
}

// escapeHTML escapes HTML special characters
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
