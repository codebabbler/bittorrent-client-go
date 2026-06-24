package torrent

import (
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/codebabbler/bittorrent-client-go/bencode"
)

// MagnetLink holds parsed magnet link data.
type MagnetLink struct {
	InfoHash    []byte
	InfoHashHex string
	Name        string
	TrackerURL  string
	TrackerURLs []string
}

// ParseMagnet parses a magnet URI and extracts info hash and tracker URL.
func ParseMagnet(uri string) (*MagnetLink, error) {
	parsedUrl, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("parsing magnet link: %w", err)
	}

	xt := parsedUrl.Query().Get("xt")
	if xt == "" {
		return nil, fmt.Errorf("missing xt parameter")
	}

	if !strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
		return nil, fmt.Errorf("unsupported or missing magnet hash format: must start with urn:btih:")
	}

	hashStr := xt[len("urn:btih:"):]
	var infoHash []byte

	switch len(hashStr) {
	case 40: // Base16 (Hex)
		infoHash, err = hex.DecodeString(hashStr)
		if err != nil {
			return nil, fmt.Errorf("decoding hex info hash: %w", err)
		}
	case 32: // Base32
		infoHash, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(hashStr))
		if err != nil {
			return nil, fmt.Errorf("decoding base32 info hash: %w", err)
		}
	default:
		return nil, fmt.Errorf("invalid info hash length: %d (expected 32 or 40)", len(hashStr))
	}

	infoHashHex := hex.EncodeToString(infoHash)

	trackers := parsedUrl.Query()["tr"]
	var primaryTracker string
	if len(trackers) > 0 {
		primaryTracker = trackers[0]
	}

	return &MagnetLink{
		InfoHash:    infoHash,
		InfoHashHex: infoHashHex,
		Name:        parsedUrl.Query().Get("dn"),
		TrackerURL:  primaryTracker,
		TrackerURLs: trackers,
	}, nil
}

// ToTorrentFile converts raw metadata (the bencoded info dict) received from
// a peer via BEP 9 into a TorrentFile. It verifies the SHA-1 hash matches.
func (m *MagnetLink) ToTorrentFile(rawMetadata []byte) (*TorrentFile, error) {
	// Verify info hash
	computedHash := sha1.Sum(rawMetadata)
	if hex.EncodeToString(computedHash[:]) != m.InfoHashHex {
		return nil, fmt.Errorf("metadata hash mismatch: computed %s, expected %s", hex.EncodeToString(computedHash[:]), m.InfoHashHex)
	}

	metaStr := string(rawMetadata)
	metaPos := 0
	infoDict, _, err := bencode.DecodeDict(metaStr, &metaPos)
	if err != nil {
		return nil, fmt.Errorf("decoding info dictionary: %w", err)
	}



	pieceLength, ok := infoDict["piece length"].(int)
	if !ok {
		return nil, fmt.Errorf("piece length not found in info dict")
	}

	pieces, ok := infoDict["pieces"].(string)
	if !ok {
		return nil, fmt.Errorf("pieces not found in info dict")
	}

	name, _ := infoDict["name"].(string)

	tf := &TorrentFile{
		Announce:    m.TrackerURL,
		InfoHash:    computedHash,
		InfoHashHex: m.InfoHashHex,
		PieceLength: pieceLength,
		Name:        name,
		Pieces:      pieces,
	}

	// Detect multi-file vs single-file
	if filesList, ok := infoDict["files"].([]interface{}); ok {
		tf.IsMultiFile = true
		totalLength := 0
		for _, f := range filesList {
			fileDict, ok := f.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid file entry in info dict")
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
		length, ok := infoDict["length"].(int)
		if !ok {
			return nil, fmt.Errorf("length not found in info dict")
		}
		tf.Length = length
	}

	return tf, nil
}
