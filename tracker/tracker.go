package tracker

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/codebabbler/bittorrent-client-go/bencode"
)

// Peer represents a peer's address from the tracker response.
type Peer struct {
	IP   net.IP
	Port uint16
}

func (p Peer) Address() string {
	return net.JoinHostPort(p.IP.String(), strconv.Itoa(int(p.Port)))
}

// GetPeers contacts the tracker and returns a list of peers.
// Dispatches to HTTP or UDP based on the tracker URL scheme.
func GetPeers(trackerURL string, infoHash []byte, left int) ([]Peer, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, fmt.Errorf("parsing tracker URL: %w", err)
	}

	switch u.Scheme {
	case "http", "https":
		return getPeersHTTP(trackerURL, infoHash, left)
	case "udp":
		return GetPeersUDP(trackerURL, infoHash, left)
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

func escapeInfoHash(infoHash []byte) string {
	hex := "0123456789abcdef"
	var res []byte
	for _, b := range infoHash {
		res = append(res, '%', hex[b>>4], hex[b&0xf])
	}
	return string(res)
}

// getPeersHTTP contacts an HTTP tracker and returns a list of peers.
func getPeersHTTP(trackerURL string, infoHash []byte, left int) ([]Peer, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}

	escapedHash := escapeInfoHash(infoHash)

	params := url.Values{}
	params.Set("peer_id", "-MT1230-rT6yUi8OpLkJ")
	params.Set("port", "6881")
	params.Set("uploaded", "0")
	params.Set("downloaded", "0")
	params.Set("left", strconv.Itoa(left))
	params.Set("compact", "1")

	queryStr := "info_hash=" + escapedHash + "&" + params.Encode()
	if u.RawQuery != "" {
		u.RawQuery += "&" + queryStr
	} else {
		u.RawQuery = queryStr
	}

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tracker returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	decoded, err := bencode.Decode(string(bodyBytes))
	if err != nil {
		return nil, err
	}

	respDict, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid response map")
	}
	if failReason, ok := respDict["failure reason"]; ok {
		return nil, fmt.Errorf("tracker error: %v", failReason)
	}

	peersVal, ok := respDict["peers"]
	if !ok {
		return nil, fmt.Errorf("no peers field found in tracker response")
	}

	var peers []Peer
	switch pVal := peersVal.(type) {
	case string:
		for i := 0; i+6 <= len(pVal); i += 6 {
			ip := net.IP(pVal[i : i+4])
			port := binary.BigEndian.Uint16([]byte(pVal[i+4 : i+6]))
			peers = append(peers, Peer{IP: ip, Port: port})
		}
	case []interface{}:
		for _, item := range pVal {
			pDict, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			ipStr, ok := pDict["ip"].(string)
			if !ok {
				continue
			}
			var portVal int
			if pInt, ok := pDict["port"].(int); ok {
				portVal = pInt
			} else if pInt64, ok := pDict["port"].(int64); ok {
				portVal = int(pInt64)
			} else {
				continue
			}
			ip := net.ParseIP(ipStr)
			if ip == nil {
				continue
			}
			if ip.To4() == nil {
				continue
			}
			peers = append(peers, Peer{IP: ip.To4(), Port: uint16(portVal)})
		}
	default:
		return nil, fmt.Errorf("unsupported peers format: %T", peersVal)
	}

	if len(peers) == 0 {
		return nil, fmt.Errorf("no peers available for this torrent")
	}

	return peers, nil
}
