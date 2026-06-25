package metrics

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// ProgressReader wraps an io.Reader and reports progress
type ProgressReader struct {
	reader         io.Reader
	total          int64
	transferred    int64
	lastReport     time.Time
	reportInterval time.Duration
	description    string
}

// NewProgressReader creates a new progress tracking reader
func NewProgressReader(r io.Reader, total int64, description string) *ProgressReader {
	return &ProgressReader{
		reader:         r,
		total:          total,
		lastReport:     time.Now(),
		reportInterval: 1 * time.Second, // Report every second
		description:    description,
	}
}

// Read implements io.Reader interface
func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	atomic.AddInt64(&pr.transferred, int64(n))

	// Report progress periodically
	if time.Since(pr.lastReport) >= pr.reportInterval {
		pr.reportProgress()
		pr.lastReport = time.Now()
	}

	// Report on completion
	if err == io.EOF && n > 0 {
		pr.reportProgress()
	}

	return n, err
}

// reportProgress prints current progress
func (pr *ProgressReader) reportProgress() {
	transferred := atomic.LoadInt64(&pr.transferred)

	if pr.total > 0 {
		percentage := float64(transferred) / float64(pr.total) * 100
		fmt.Printf("[Progress] %s: %.2f%% (%s / %s)\n",
			pr.description,
			percentage,
			FormatBytes(transferred),
			FormatBytes(pr.total))
	} else {
		fmt.Printf("[Progress] %s: %s transferred\n",
			pr.description,
			FormatBytes(transferred))
	}
}

// GetTransferred returns the number of bytes transferred
func (pr *ProgressReader) GetTransferred() int64 {
	return atomic.LoadInt64(&pr.transferred)
}

// ProgressWriter wraps an io.Writer and reports progress
type ProgressWriter struct {
	writer         io.Writer
	total          int64
	transferred    int64
	lastReport     time.Time
	reportInterval time.Duration
	description    string
}

// NewProgressWriter creates a new progress tracking writer
func NewProgressWriter(w io.Writer, total int64, description string) *ProgressWriter {
	return &ProgressWriter{
		writer:         w,
		total:          total,
		lastReport:     time.Now(),
		reportInterval: 1 * time.Second,
		description:    description,
	}
}

// Write implements io.Writer interface
func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	atomic.AddInt64(&pw.transferred, int64(n))

	// Report progress periodically
	if time.Since(pw.lastReport) >= pw.reportInterval {
		pw.reportProgress()
		pw.lastReport = time.Now()
	}

	return n, err
}

// reportProgress prints current progress
func (pw *ProgressWriter) reportProgress() {
	transferred := atomic.LoadInt64(&pw.transferred)

	if pw.total > 0 {
		percentage := float64(transferred) / float64(pw.total) * 100
		fmt.Printf("[Progress] %s: %.2f%% (%s / %s)\n",
			pw.description,
			percentage,
			FormatBytes(transferred),
			FormatBytes(pw.total))
	} else {
		fmt.Printf("[Progress] %s: %s transferred\n",
			pw.description,
			FormatBytes(transferred))
	}
}

// GetTransferred returns the number of bytes transferred
func (pw *ProgressWriter) GetTransferred() int64 {
	return atomic.LoadInt64(&pw.transferred)
}

// FormatBytes formats bytes into human-readable format
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.2f %s", float64(bytes)/float64(div), units[exp])
}
