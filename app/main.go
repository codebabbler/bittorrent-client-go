package main

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"unicode"
	// bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

func decodeString(input string, pos *int) (string, error) {
	start := *pos

	for *pos < len(input) && input[*pos] != ':' {
		if !unicode.IsDigit(rune(input[*pos])) {
			return "", fmt.Errorf("invalid bencoded string length")
		}
		(*pos)++
	}

	if *pos >= len(input) {
		return "", fmt.Errorf("invalid bencoded string: missing colon")
	}

	lengthStr := input[start:*pos]
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", err
	}

	(*pos)++ // skip ':'

	if *pos+length > len(input) {
		return "", fmt.Errorf("invalid bencoded string: length exceeds data")
	}

	result := input[*pos : *pos+length]
	*pos += length

	return result, nil
}

func decodeInteger(input string, pos *int) (int, error) {
	(*pos)++ // skip 'i'
	start := *pos

	for *pos < len(input) && input[*pos] != 'e' {
		(*pos)++
	}

	if *pos >= len(input) {
		return 0, fmt.Errorf("Invalid bencoded integer")
	}

	number, err := strconv.Atoi(input[start:*pos])
	if err != nil {
		return 0, err
	}

	(*pos)++ // skip 'e'
	return number, nil
}

func decodeList(input string, pos *int) ([]interface{}, error) {
	(*pos)++ // skip 'l'

	result := []interface{}{}

	for *pos < len(input) {
		if input[*pos] == 'e' {
			(*pos)++ // consume 'e'
			return result, nil
		}

		value, err := decode(input, pos)
		if err != nil {
			return nil, err
		}

		result = append(result, value)
	}

	return nil, fmt.Errorf("invalid bencoded list: missing terminating 'e'")
}

// decodeDict returns the parsed map and a map of raw bencoded bytes per key
func decodeDict(input string, pos *int) (map[string]interface{}, map[string]string, error) {
	(*pos)++ // skip 'd'

	result := make(map[string]interface{})
	rawValues := make(map[string]string)

	for *pos < len(input) {
		if input[*pos] == 'e' {
			(*pos)++ // consume 'e'
			return result, rawValues, nil
		}

		// Keys must always be strings in bencode
		if !unicode.IsDigit(rune(input[*pos])) {
			return nil, nil, fmt.Errorf("invalid bencoded dict: key must be a string")
		}

		key, err := decodeString(input, pos)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid bencoded dict key: %w", err)
		}

		startPos := *pos
		value, err := decode(input, pos)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid bencoded dict value for key %q: %w", key, err)
		}

		result[key] = value
		rawValues[key] = input[startPos:*pos]
	}

	return nil, nil, fmt.Errorf("invalid bencoded dict: missing terminating 'e'")
}

// Example:
// - 5:hello -> hello
// - 10:hello12345 -> hello12345
func decodeBencode(bencodedString string) (interface{}, error) {
	if len(bencodedString) == 0 {
		return "", fmt.Errorf("empty bencoded string")
	}

	pos := 0
	return decode(bencodedString, &pos)
}

func decode(input string, pos *int) (interface{}, error) {
	if *pos >= len(input) {
		return nil, fmt.Errorf("unexpected end of input")
	}

	switch char := input[*pos]; {
	case unicode.IsDigit(rune(char)):
		return decodeString(input, pos)
	case char == 'i':
		return decodeInteger(input, pos)
	case char == 'l':
		return decodeList(input, pos)
	case char == 'd':
		dict, _, err := decodeDict(input, pos)
		return dict, err
	default:
		return "", fmt.Errorf("unsupported bencode type: %q", char)
	}
}

func getRequestToTracker(trackerUrl string, 
							infoHash string, 
							peerId string, 
							port int, 
							uploaded int, 
							downloaded int, 
							left int) (*http.Response,error) {
	baseUrl := trackerUrl
	params := url.Values{}
	params.Set("info_hash", infoHash)
	params.Set("peer_id", peerId)
	params.Set("port", strconv.Itoa(port))
	params.Set("uploaded", strconv.Itoa(uploaded))
	params.Set("downloaded", strconv.Itoa(downloaded))
	params.Set("left", strconv.Itoa(left))
	params.Set("compact", strconv.Itoa(1))
	resp, err := http.Get(baseUrl + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	// Note: caller is responsible for closing resp.Body

	
	return resp, nil
}

func main() {
	fmt.Fprintln(os.Stderr, "Logs from your program will appear here!")

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: ./runner.sh <command> [args]")
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "decode":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh decode <bencoded_value>")
			os.Exit(1)
		}

		decoded, err := decodeBencode(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		jsonOutput, err := json.Marshal(decoded)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error marshalling JSON:", err)
			os.Exit(1)
		}

		fmt.Println(string(jsonOutput))
	case "info":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh info <torrent_file>")
			os.Exit(1)
		}

		data, err := os.ReadFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		dataStr := string(data)
		if len(dataStr) == 0 || dataStr[0] != 'd' {
			fmt.Fprintln(os.Stderr, "Error: torrent file is not a bencoded dictionary")
			os.Exit(1)
		}

		// Decode the top-level dict, getting both parsed values and raw bytes in one pass
		pos := 0
		torrent, rawValues, err := decodeDict(dataStr, &pos)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		announce, announceOk := torrent["announce"].(string)
		if !announceOk {
			fmt.Fprintln(os.Stderr, "Error: announce not found")
			os.Exit(1)
		}

		info, infoOk := torrent["info"].(map[string]interface{})
		if !infoOk {
			fmt.Fprintln(os.Stderr, "Error: info not found")
			os.Exit(1)
		}

		// Raw bytes captured during decode — no re-encoding needed
		infoHash := sha1.Sum([]byte(rawValues["info"]))

		length, lengthOk := info["length"].(int)
		if !lengthOk {
			fmt.Fprintln(os.Stderr, "Error: length not found")
			os.Exit(1)
		}

		piecesStr, piecesOk := info["pieces"].(string)
		if !piecesOk {
			fmt.Fprintln(os.Stderr, "Error: pieces not found")
			os.Exit(1)
		}


		fmt.Println("Tracker URL:", announce)
		fmt.Println("Length:", length)
		fmt.Println("Piece Length:", info["piece length"])
		fmt.Println("Info Hash:", hex.EncodeToString(infoHash[:]))

		fmt.Println("Piece Hashes:")
		for i := 0; i < len(piecesStr); i += 20 {
			end := i + 20
			if end > len(piecesStr) {
				end = len(piecesStr)
			}
			fmt.Println(hex.EncodeToString([]byte(piecesStr[i:end])))
		}

	case "peers":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh peers <torrent_file>")
			os.Exit(1)
		}

		data, err := os.ReadFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		dataStr := string(data)
		if len(dataStr) == 0 || dataStr[0] != 'd' {
			fmt.Fprintln(os.Stderr, "Error: torrent file is not a bencoded dictionary")
			os.Exit(1)
		}

		pos := 0
		torrent, rawValues, err := decodeDict(dataStr, &pos)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		announce, announceOk := torrent["announce"].(string)
		if !announceOk {
			fmt.Fprintln(os.Stderr, "Error: announce not found")
			os.Exit(1)
		}

		info, infoOk := torrent["info"].(map[string]interface{})
		if !infoOk {
			fmt.Fprintln(os.Stderr, "Error: info not found")
			os.Exit(1)
		}

		infoHash := sha1.Sum([]byte(rawValues["info"]))

		length, lengthOk := info["length"].(int)
		if !lengthOk {
			fmt.Fprintln(os.Stderr, "Error: length not found")
			os.Exit(1)
		}

		resp, err := getRequestToTracker(announce, string(infoHash[:]), "-MT1230-rT6yUi8OpLkJ", 6881, 0, 0, length)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		decoded, err := decodeBencode(string(bodyBytes))
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		peers := decoded.(map[string]interface{})["peers"].(string)
		for i := 0; i < len(peers); i += 6 {
			ip := net.IP(peers[i : i+4])
			port := binary.BigEndian.Uint16([]byte(peers[i+4 : i+6]))
			fmt.Printf("%s:%d\n", ip, port)
		}

	case "handshake":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh handshake <torrent_file> <peer_address>")
			os.Exit(1)
		}

		torrentFile := os.Args[2]
		peerAddress := os.Args[3]

		data, err := os.ReadFile(torrentFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		dataStr := string(data)
		pos := 0
		_, rawValues, err := decodeDict(dataStr, &pos)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		infoHash := sha1.Sum([]byte(rawValues["info"]))

		// Connect to peer
		conn, err := net.Dial("tcp", peerAddress)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error connecting:", err)
			os.Exit(1)
		}
		defer conn.Close()

		// Build 68-byte handshake
		var handshake [68]byte
		handshake[0] = 19
		copy(handshake[1:20], []byte("BitTorrent protocol"))

		// reserved bytes [20-28] are already zero
		copy(handshake[28:48], infoHash[:])

		var peerId [20]byte
		_, err = rand.Read(peerId[:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error generating peer ID:", err)
			os.Exit(1)
		}
		copy(handshake[48:68], peerId[:])

		// Send handshake
		_, err = conn.Write(handshake[:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error sending handshake:", err)
			os.Exit(1)
		}

		// Receive handshake
		var response [68]byte
		_, err = io.ReadFull(conn, response[:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error reading handshake:", err)
			os.Exit(1)
		}

		// Print peer ID (last 20 bytes)
		receivedPeerId := response[48:68]
		fmt.Println("Peer ID:", hex.EncodeToString(receivedPeerId))

	case "download_piece":
		if len(os.Args) < 6 || os.Args[2] != "-o" {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh download_piece -o <output_file> <torrent_file> <piece_index>")
			os.Exit(1)
		}

		outputPath := os.Args[3]
		torrentFile := os.Args[4]
		pieceIndex, err := strconv.Atoi(os.Args[5])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: invalid piece index")
			os.Exit(1)
		}

		// Parse torrent file
		data, err := os.ReadFile(torrentFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		dataStr := string(data)
		pos := 0
		torrent, rawValues, err := decodeDict(dataStr, &pos)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		announce := torrent["announce"].(string)
		info := torrent["info"].(map[string]interface{})
		infoHash := sha1.Sum([]byte(rawValues["info"]))
		totalLength := info["length"].(int)
		normalPieceLength := info["piece length"].(int)
		piecesStr := info["pieces"].(string)

		// Calculate this piece's actual size (last piece may be smaller)
		pieceLength := normalPieceLength
		totalPieces := (totalLength + normalPieceLength - 1) / normalPieceLength
		if pieceIndex == totalPieces-1 {
			pieceLength = totalLength - (pieceIndex * normalPieceLength)
		}

		// Get peers from tracker
		resp, err := getRequestToTracker(announce, string(infoHash[:]), "-MT1230-rT6yUi8OpLkJ", 6881, 0, 0, totalLength)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		decoded, err := decodeBencode(string(bodyBytes))
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		peersStr := decoded.(map[string]interface{})["peers"].(string)
		peerIP := net.IP(peersStr[0:4])
		peerPort := binary.BigEndian.Uint16([]byte(peersStr[4:6]))
		peerAddress := net.JoinHostPort(peerIP.String(), strconv.Itoa(int(peerPort)))

		// TCP connection & handshake
		conn, err := net.Dial("tcp", peerAddress)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error connecting:", err)
			os.Exit(1)
		}
		defer conn.Close()

		var handshake [68]byte
		handshake[0] = 19
		copy(handshake[1:20], []byte("BitTorrent protocol"))
		copy(handshake[28:48], infoHash[:])
		var peerId [20]byte
		_, err = rand.Read(peerId[:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		copy(handshake[48:68], peerId[:])

		_, err = conn.Write(handshake[:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error sending handshake:", err)
			os.Exit(1)
		}

		var handshakeResp [68]byte
		_, err = io.ReadFull(conn, handshakeResp[:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error reading handshake:", err)
			os.Exit(1)
		}

		// Receive bitfield (message ID = 5)
		lengthBuf := make([]byte, 4)
		_, err = io.ReadFull(conn, lengthBuf)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error reading bitfield length:", err)
			os.Exit(1)
		}
		msgLen := binary.BigEndian.Uint32(lengthBuf)
		if msgLen > 0 {
			msgBuf := make([]byte, msgLen)
			_, err = io.ReadFull(conn, msgBuf)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error reading bitfield:", err)
				os.Exit(1)
			}
			// msgBuf[0] should be 5 (bitfield), ignore payload
		}

		// Send interested (message ID = 2)
		interested := make([]byte, 5)
		binary.BigEndian.PutUint32(interested[0:4], 1) // length = 1
		interested[4] = 2                               // message ID
		_, err = conn.Write(interested)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error sending interested:", err)
			os.Exit(1)
		}

		// Wait for unchoke (message ID = 1)
		for {
			_, err = io.ReadFull(conn, lengthBuf)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error waiting for unchoke:", err)
				os.Exit(1)
			}
			msgLen = binary.BigEndian.Uint32(lengthBuf)
			if msgLen == 0 {
				continue // keepalive
			}
			msgBuf := make([]byte, msgLen)
			_, err = io.ReadFull(conn, msgBuf)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error reading unchoke:", err)
				os.Exit(1)
			}
			if msgBuf[0] == 1 { // unchoke
				break
			}
		}

		// Request blocks and receive piece data
		blockSize := 16384 // 16 KiB
		pieceData := make([]byte, pieceLength)
		totalBlocks := (pieceLength + blockSize - 1) / blockSize

		for i := 0; i < totalBlocks; i++ {
			offset := i * blockSize
			length := blockSize
			if offset+length > pieceLength {
				length = pieceLength - offset
			}

			// Send request (message ID = 6, payload = 12 bytes)
			request := make([]byte, 17)
			binary.BigEndian.PutUint32(request[0:4], 13) // length = 13
			request[4] = 6                                // message ID = request
			binary.BigEndian.PutUint32(request[5:9], uint32(pieceIndex))
			binary.BigEndian.PutUint32(request[9:13], uint32(offset))
			binary.BigEndian.PutUint32(request[13:17], uint32(length))
			_, err = conn.Write(request)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error sending request:", err)
				os.Exit(1)
			}
		}

		// Receive all blocks
		blocksReceived := 0
		for blocksReceived < totalBlocks {
			_, err = io.ReadFull(conn, lengthBuf)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error reading piece length:", err)
				os.Exit(1)
			}
			msgLen = binary.BigEndian.Uint32(lengthBuf)
			if msgLen == 0 {
				continue // keepalive
			}
			msgBuf := make([]byte, msgLen)
			_, err = io.ReadFull(conn, msgBuf)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error reading piece:", err)
				os.Exit(1)
			}
			if msgBuf[0] != 7 { // not a piece message
				continue
			}
			begin := binary.BigEndian.Uint32(msgBuf[5:9])
			block := msgBuf[9:]
			copy(pieceData[begin:], block)
			blocksReceived++
		}

		// Verify piece hash
		expectedHash := piecesStr[pieceIndex*20 : (pieceIndex+1)*20]
		actualHash := sha1.Sum(pieceData)
		if string(actualHash[:]) != expectedHash {
			fmt.Fprintln(os.Stderr, "Error: piece hash mismatch")
			os.Exit(1)
		}

		// Save to disk
		err = os.WriteFile(outputPath, pieceData, 0644)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error writing file:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Piece %d downloaded to %s.\n", pieceIndex, outputPath)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		os.Exit(1)
	}
}
