package dht

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/codebabbler/bittorrent-client-go/bencode"
)

// KRPC message types
const (
	MsgTypeQuery    = "q"
	MsgTypeResponse = "r"
	MsgTypeError    = "e"
)

// encodeBencode encodes a Go value into a bencoded string.
// Supports string, int, []interface{}, map[string]interface{}.
func encodeBencode(v interface{}) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%d:%s", len(val), val)
	case int:
		return fmt.Sprintf("i%de", val)
	case []interface{}:
		var sb strings.Builder
		sb.WriteByte('l')
		for _, item := range val {
			sb.WriteString(encodeBencode(item))
		}
		sb.WriteByte('e')
		return sb.String()
	case map[string]interface{}:
		var sb strings.Builder
		sb.WriteByte('d')
		// Bencode requires sorted keys
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
			sb.WriteString(encodeBencode(k))
			sb.WriteString(encodeBencode(val[k]))
		}
		sb.WriteByte('e')
		return sb.String()
	default:
		return ""
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// GenerateTransactionID creates a short random transaction ID.
func GenerateTransactionID() string {
	return strconv.Itoa(rand.Intn(65536))
}

// BuildPingQuery builds a bencoded ping query.
func BuildPingQuery(txnID string, nodeID NodeID) []byte {
	msg := map[string]interface{}{
		"t": txnID,
		"y": MsgTypeQuery,
		"q": "ping",
		"a": map[string]interface{}{
			"id": string(nodeID[:]),
		},
	}
	return []byte(encodeBencode(msg))
}

// BuildFindNodeQuery builds a bencoded find_node query.
func BuildFindNodeQuery(txnID string, nodeID NodeID, target NodeID) []byte {
	msg := map[string]interface{}{
		"t": txnID,
		"y": MsgTypeQuery,
		"q": "find_node",
		"a": map[string]interface{}{
			"id":     string(nodeID[:]),
			"target": string(target[:]),
		},
	}
	return []byte(encodeBencode(msg))
}

// BuildGetPeersQuery builds a bencoded get_peers query.
func BuildGetPeersQuery(txnID string, nodeID NodeID, infoHash [20]byte) []byte {
	msg := map[string]interface{}{
		"t": txnID,
		"y": MsgTypeQuery,
		"q": "get_peers",
		"a": map[string]interface{}{
			"id":        string(nodeID[:]),
			"info_hash": string(infoHash[:]),
		},
	}
	return []byte(encodeBencode(msg))
}

// KRPCResponse holds a parsed KRPC response.
type KRPCResponse struct {
	TransactionID string
	Type          string // "r" or "e"
	// Response fields (for type "r")
	ID    NodeID
	Nodes []Node // from "nodes" key (compact node info)
	// Peer values from "values" key (compact peer info, 6 bytes each)
	Values []string
	Token  string
	// Error fields (for type "e")
	ErrorCode int
	ErrorMsg  string
}

// ParseKRPCResponse decodes a bencoded KRPC response.
func ParseKRPCResponse(data []byte) (*KRPCResponse, error) {
	decoded, err := bencode.Decode(string(data))
	if err != nil {
		return nil, fmt.Errorf("decoding krpc response: %w", err)
	}

	msg, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("krpc response is not a dict")
	}

	resp := &KRPCResponse{}

	if t, ok := msg["t"].(string); ok {
		resp.TransactionID = t
	}
	if y, ok := msg["y"].(string); ok {
		resp.Type = y
	}

	// Error response
	if resp.Type == MsgTypeError {
		if e, ok := msg["e"].([]interface{}); ok && len(e) >= 2 {
			if code, ok := e[0].(int); ok {
				resp.ErrorCode = code
			}
			if emsg, ok := e[1].(string); ok {
				resp.ErrorMsg = emsg
			}
		}
		return resp, nil
	}

	// Normal response
	if r, ok := msg["r"].(map[string]interface{}); ok {
		if id, ok := r["id"].(string); ok && len(id) == 20 {
			copy(resp.ID[:], id)
		}
		if token, ok := r["token"].(string); ok {
			resp.Token = token
		}
		if nodes, ok := r["nodes"].(string); ok {
			resp.Nodes = CompactNodesDecode([]byte(nodes))
		}
		if values, ok := r["values"].([]interface{}); ok {
			for _, v := range values {
				if vs, ok := v.(string); ok {
					resp.Values = append(resp.Values, vs)
				}
			}
		}
	}

	return resp, nil
}
