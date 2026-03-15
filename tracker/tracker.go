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

// getPeersHTTP contacts an HTTP tracker and returns a list of peers.
func getPeersHTTP(trackerURL string, infoHash []byte, left int) ([]Peer, error) {
	params := url.Values{}
	params.Set("info_hash", string(infoHash))
	params.Set("peer_id", "-MT1230-rT6yUi8OpLkJ")
	params.Set("port", "6881")
	params.Set("uploaded", "0")
	params.Set("downloaded", "0")
	params.Set("left", strconv.Itoa(left))
	params.Set("compact", "1")

	resp, err := http.Get(trackerURL + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	decoded, err := bencode.Decode(string(bodyBytes))
	if err != nil {
		return nil, err
	}

	respDict := decoded.(map[string]interface{})
	if failReason, ok := respDict["failure reason"]; ok {
		return nil, fmt.Errorf("tracker error: %v", failReason)
	}

	peersStr, ok := respDict["peers"].(string)
	if !ok || len(peersStr) == 0 {
		return nil, fmt.Errorf("no peers available for this torrent")
	}

	var peers []Peer
	for i := 0; i+6 <= len(peersStr); i += 6 {
		ip := net.IP(peersStr[i : i+4])
		port := binary.BigEndian.Uint16([]byte(peersStr[i+4 : i+6]))
		peers = append(peers, Peer{IP: ip, Port: port})
	}

	return peers, nil
}
