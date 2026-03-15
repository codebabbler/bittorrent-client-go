package torrent

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/codebabbler/bittorrent-client-go/bencode"
)

// FileEntry represents a single file in a multi-file torrent.
type FileEntry struct {
	Length int
	Path   []string // e.g. ["subdir", "file.txt"]
}

// TorrentFile holds parsed torrent metadata.
type TorrentFile struct {
	Announce    string
	InfoHash    [20]byte
	InfoHashHex string
	PieceLength int
	Length      int      // total length (sum of all files for multi-file)
	Name        string
	Pieces      string   // raw 20-byte concatenated piece hashes
	Files       []FileEntry // non-nil for multi-file torrents
	IsMultiFile bool
}

// ParseFile reads and parses a .torrent file.
func ParseFile(path string) (*TorrentFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	dataStr := string(data)
	if len(dataStr) == 0 || dataStr[0] != 'd' {
		return nil, fmt.Errorf("torrent file is not a bencoded dictionary")
	}

	pos := 0
	torrent, rawValues, err := bencode.DecodeDict(dataStr, &pos)
	if err != nil {
		return nil, err
	}

	announce, ok := torrent["announce"].(string)
	if !ok {
		return nil, fmt.Errorf("announce not found")
	}

	info, ok := torrent["info"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("info not found")
	}

	infoHash := sha1.Sum([]byte(rawValues["info"]))

	pieces, ok := info["pieces"].(string)
	if !ok {
		return nil, fmt.Errorf("pieces not found")
	}

	pieceLength, ok := info["piece length"].(int)
	if !ok {
		return nil, fmt.Errorf("piece length not found")
	}

	name, _ := info["name"].(string)

	tf := &TorrentFile{
		Announce:    announce,
		InfoHash:    infoHash,
		InfoHashHex: hex.EncodeToString(infoHash[:]),
		PieceLength: pieceLength,
		Name:        name,
		Pieces:      pieces,
	}

	// Detect multi-file vs single-file
	if filesList, ok := info["files"].([]interface{}); ok {
		tf.IsMultiFile = true
		totalLength := 0
		for _, f := range filesList {
			fileDict, ok := f.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid file entry")
			}
			fl, ok := fileDict["length"].(int)
			if !ok {
				return nil, fmt.Errorf("file entry missing length")
			}
			var path []string
			pathList, ok := fileDict["path"].([]interface{})
			if !ok {
				return nil, fmt.Errorf("file entry missing path")
			}
			for _, p := range pathList {
				ps, ok := p.(string)
				if !ok {
					return nil, fmt.Errorf("invalid path component")
				}
				path = append(path, ps)
			}
			tf.Files = append(tf.Files, FileEntry{Length: fl, Path: path})
			totalLength += fl
		}
		tf.Length = totalLength
	} else {
		length, ok := info["length"].(int)
		if !ok {
			return nil, fmt.Errorf("length not found")
		}
		tf.Length = length
	}

	return tf, nil
}

// PieceHashes returns the hex-encoded SHA-1 hashes for each piece.
func (tf *TorrentFile) PieceHashes() []string {
	var hashes []string
	for i := 0; i < len(tf.Pieces); i += 20 {
		end := i + 20
		if end > len(tf.Pieces) {
			end = len(tf.Pieces)
		}
		hashes = append(hashes, hex.EncodeToString([]byte(tf.Pieces[i:end])))
	}
	return hashes
}
