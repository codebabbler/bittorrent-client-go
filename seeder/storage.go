package seeder

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codebabbler/bittorrent-client-go/torrent"
)

// LoadPieces reads the file(s) described by the torrent and splits them into piece-sized chunks.
func LoadPieces(dataPath string, tf *torrent.TorrentFile) ([][]byte, error) {
	var rawData []byte

	if tf.IsMultiFile {
		// Multi-file: read all files in order and concatenate
		for _, f := range tf.Files {
			pathParts := append([]string{dataPath}, f.Path...)
			filePath := filepath.Join(pathParts...)
			data, err := os.ReadFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", filePath, err)
			}
			rawData = append(rawData, data...)
		}
	} else {
		// Single-file
		data, err := os.ReadFile(dataPath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", dataPath, err)
		}
		rawData = data
	}

	if len(rawData) != tf.Length {
		return nil, fmt.Errorf("data length mismatch: expected %d, got %d", tf.Length, len(rawData))
	}

	// Split into pieces and verify hashes
	var pieces [][]byte
	totalPieces := (tf.Length + tf.PieceLength - 1) / tf.PieceLength

	for i := 0; i < totalPieces; i++ {
		start := i * tf.PieceLength
		end := start + tf.PieceLength
		if end > len(rawData) {
			end = len(rawData)
		}

		piece := rawData[start:end]

		// Verify piece hash
		expectedHash := tf.Pieces[i*20 : (i+1)*20]
		actualHash := sha1.Sum(piece)
		if string(actualHash[:]) != expectedHash {
			return nil, fmt.Errorf("hash mismatch for piece %d", i)
		}

		pieces = append(pieces, piece)
	}

	return pieces, nil
}

// BuildBitfield creates a bitfield with all bits set (we have all pieces).
func BuildBitfield(numPieces int) []byte {
	numBytes := (numPieces + 7) / 8
	bitfield := make([]byte, numBytes)
	for i := 0; i < numPieces; i++ {
		byteIndex := i / 8
		bitIndex := 7 - (i % 8)
		bitfield[byteIndex] |= 1 << uint(bitIndex)
	}
	return bitfield
}
