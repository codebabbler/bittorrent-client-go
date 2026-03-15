package download

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/codebabbler/bittorrent-client-go/torrent"
)

// WriteFiles splits flat piece data into individual files for multi-file torrents.
// It creates the directory tree under outputDir/tf.Name/.
func WriteFiles(outputDir string, tf *torrent.TorrentFile, data []byte) error {
	baseDir := filepath.Join(outputDir, tf.Name)

	offset := 0
	for _, f := range tf.Files {
		// Build full output path
		pathParts := append([]string{baseDir}, f.Path...)
		filePath := filepath.Join(pathParts...)

		// Create parent directories
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}

		// Extract this file's bytes
		end := offset + f.Length
		if end > len(data) {
			end = len(data)
		}
		fileData := data[offset:end]
		offset = end

		// Write file
		if err := os.WriteFile(filePath, fileData, 0644); err != nil {
			return fmt.Errorf("writing %s: %w", filePath, err)
		}
		fmt.Fprintf(os.Stderr, "  Written: %s (%d bytes)\n", filePath, len(fileData))
	}

	return nil
}
