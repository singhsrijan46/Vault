package quota

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AdityaKrSingh26/PeerVault/internal/metrics"
	"github.com/AdityaKrSingh26/PeerVault/internal/storage"
)

// QuotaConfig stores storage quota configuration
type QuotaConfig struct {
	MaxStorageBytes int64  `json:"max_storage_bytes"`
	StorageRoot     string `json:"storage_root"`
}

// QuotaManager manages storage quotas
type QuotaManager struct {
	config     QuotaConfig
	configPath string
}

// NewQuotaManager creates a new quota manager
func NewQuotaManager(storageRoot string) *QuotaManager {
	configPath := filepath.Join(storageRoot, ".quota_config.json")
	return &QuotaManager{
		config: QuotaConfig{
			StorageRoot: storageRoot,
		},
		configPath: configPath,
	}
}

// LoadOrCreate loads existing quota config or creates a new one interactively
func (qm *QuotaManager) LoadOrCreate() error {
	// Try to load existing config
	if _, err := os.Stat(qm.configPath); err == nil {
		return qm.load()
	}

	// Config doesn't exist, create interactively
	fmt.Println("\n=== Storage Quota Configuration ===")
	fmt.Println("This is the first time running PeerVault with this storage location.")
	fmt.Println("Please configure the maximum storage quota for this node.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("Enter maximum storage size (e.g., 1GB, 500MB, 10GB): ")
		if !scanner.Scan() {
			return fmt.Errorf("failed to read input")
		}

		input := strings.TrimSpace(scanner.Text())
		bytes, err := parseStorageSize(input)
		if err != nil {
			fmt.Printf("Invalid format: %v. Please try again.\n", err)
			continue
		}

		qm.config.MaxStorageBytes = bytes
		fmt.Printf("Storage quota set to: %s (%d bytes)\n", metrics.FormatBytes(bytes), bytes)
		break
	}

	// Save configuration
	return qm.save()
}

// load loads quota config from file
func (qm *QuotaManager) load() error {
	data, err := os.ReadFile(qm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read quota config: %w", err)
	}

	if err := json.Unmarshal(data, &qm.config); err != nil {
		return fmt.Errorf("failed to parse quota config: %w", err)
	}

	return nil
}

// save saves quota config to file
func (qm *QuotaManager) save() error {
	// Ensure directory exists
	dir := filepath.Dir(qm.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(qm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal quota config: %w", err)
	}

	if err := os.WriteFile(qm.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write quota config: %w", err)
	}

	return nil
}

// GetMaxStorage returns the maximum storage quota in bytes
func (qm *QuotaManager) GetMaxStorage() int64 {
	return qm.config.MaxStorageBytes
}

// GetCurrentUsage calculates current storage usage
func (qm *QuotaManager) GetCurrentUsage(storageRoot string) (int64, error) {
	var totalSize int64

	err := filepath.Walk(storageRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to calculate storage usage: %w", err)
	}

	return totalSize, nil
}

// CheckQuota checks if there's enough space for a new file
func (qm *QuotaManager) CheckQuota(storageRoot string, newFileSize int64) (bool, int64, error) {
	currentUsage, err := qm.GetCurrentUsage(storageRoot)
	if err != nil {
		return false, 0, err
	}

	availableSpace := qm.config.MaxStorageBytes - currentUsage
	return newFileSize <= availableSpace, availableSpace, nil
}

// GetStorageStats returns storage statistics
func (qm *QuotaManager) GetStorageStats(storageRoot string) (used int64, total int64, available int64, err error) {
	used, err = qm.GetCurrentUsage(storageRoot)
	if err != nil {
		return 0, 0, 0, err
	}

	total = qm.config.MaxStorageBytes
	available = total - used
	if available < 0 {
		available = 0
	}

	return used, total, available, nil
}

// parseStorageSize parses human-readable storage size (e.g., "1GB", "500MB")
func parseStorageSize(input string) (int64, error) {
	input = strings.ToUpper(strings.TrimSpace(input))

	// Extract number and unit
	var numStr string
	var unit string

	for i, c := range input {
		if c >= '0' && c <= '9' || c == '.' {
			numStr += string(c)
		} else {
			unit = input[i:]
			break
		}
	}

	if numStr == "" {
		return 0, fmt.Errorf("no number found")
	}

	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %w", err)
	}

	unit = strings.TrimSpace(unit)

	var multiplier int64
	switch unit {
	case "B", "BYTES":
		multiplier = 1
	case "KB", "K":
		multiplier = 1024
	case "MB", "M":
		multiplier = 1024 * 1024
	case "GB", "G":
		multiplier = 1024 * 1024 * 1024
	case "TB", "T":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown unit: %s (use B, KB, MB, GB, or TB)", unit)
	}

	return int64(num * float64(multiplier)), nil
}

// PromptDeleteFiles shows list of files and asks user which to delete
func PromptDeleteFiles(store *storage.Store, nodeID string, requiredSpace int64) ([]string, error) {
	files, err := store.List(nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files to delete")
	}

	fmt.Printf("\nInsufficient storage space. Need to free up at least %s\n", metrics.FormatBytes(requiredSpace))
	fmt.Println("\nFiles available for deletion:")
	fmt.Println("┌────┬─────────────────────────────────────┬─────────────┬──────────────────────┐")
	fmt.Println("│ #  │ Filename                            │ Size        │ Hash (first 8)       │")
	fmt.Println("├────┼─────────────────────────────────────┼─────────────┼──────────────────────┤")

	for i, file := range files {
		filename := file.Key
		if len(filename) > 35 {
			filename = filename[:32] + "..."
		}
		hashShort := file.Hash
		if len(hashShort) > 8 {
			hashShort = hashShort[:8]
		}
		fmt.Printf("│ %-2d │ %-35s │ %-11s │ %-20s │\n",
			i+1, filename, metrics.FormatBytes(file.Size), hashShort)
	}
	fmt.Println("└────┴─────────────────────────────────────┴─────────────┴──────────────────────┘")

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("\nEnter file numbers to delete (comma-separated, e.g., 1,3,5) or 'cancel': ")

	if !scanner.Scan() {
		return nil, fmt.Errorf("failed to read input")
	}

	input := strings.TrimSpace(scanner.Text())
	if strings.ToLower(input) == "cancel" {
		return nil, fmt.Errorf("deletion cancelled by user")
	}

	// Parse file numbers
	parts := strings.Split(input, ",")
	filesToDelete := make([]string, 0)
	var totalFreed int64

	for _, part := range parts {
		numStr := strings.TrimSpace(part)
		num, err := strconv.Atoi(numStr)
		if err != nil || num < 1 || num > len(files) {
			fmt.Printf("Warning: Invalid file number '%s', skipping\n", numStr)
			continue
		}

		idx := num - 1
		filesToDelete = append(filesToDelete, files[idx].Key)
		totalFreed += files[idx].Size
	}

	if len(filesToDelete) == 0 {
		return nil, fmt.Errorf("no valid files selected")
	}

	fmt.Printf("\nWill delete %d file(s), freeing %s\n", len(filesToDelete), metrics.FormatBytes(totalFreed))

	if totalFreed < requiredSpace {
		fmt.Printf("Warning: This will only free %s, but %s is needed\n",
			metrics.FormatBytes(totalFreed), metrics.FormatBytes(requiredSpace))
	}

	fmt.Print("Confirm deletion? (yes/no): ")
	if !scanner.Scan() {
		return nil, fmt.Errorf("failed to read confirmation")
	}

	confirmation := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if confirmation != "yes" && confirmation != "y" {
		return nil, fmt.Errorf("deletion not confirmed")
	}

	return filesToDelete, nil
}

// SetMaxStorage sets the maximum storage limit (useful for testing)
func (qm *QuotaManager) SetMaxStorage(bytes int64) {
	qm.config.MaxStorageBytes = bytes
}

// SetStorageRoot sets the storage root path
func (qm *QuotaManager) SetStorageRoot(root string) {
	qm.config.StorageRoot = root
}
