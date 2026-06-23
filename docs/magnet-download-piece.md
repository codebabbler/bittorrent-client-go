# Downloading a Piece via Magnet Link (magnet_download_piece)

## Overview

This command combines the **metadata exchange** from `magnet_info` with the **piece download** logic from `download_piece`. Instead of reading piece metadata from a `.torrent` file, your client fetches it from peers via the extension protocol, then downloads the requested piece using the standard BitTorrent block request/response flow.

---

## Full Connection Flow

```
You                                     Peer
 │                                       │
 │──── Base Handshake (ext bit set) ────▶│
 │◀──── Base Handshake ────────────────│
 │                                       │
 │◀──── Bitfield (msg 5) ──────────────│
 │                                       │
 │── Ext Handshake (msg 20, ext 0) ───▶│
 │◀── Ext Handshake (msg 20, ext 0) ───│  → get peer's ut_metadata ID
 │                                       │
 │── Metadata Request (msg_type 0) ───▶│
 │◀── Metadata Data (msg_type 1) ──────│  → get info dict (length, pieces, etc.)
 │                                       │
 │── Interested (msg 2) ──────────────▶│
 │◀── Unchoke (msg 1) ────────────────│
 │                                       │
 │── Request blocks (msg 6) ──────────▶│  (16 KiB each)
 │◀── Piece data (msg 7) ─────────────│
 │   ... repeat for all blocks ...       │
 │                                       │
 │  Verify SHA-1 hash, save to disk      │
```

---

## Command Format

```bash
./runner.sh magnet_download_piece -o <output_file> <magnet_link> <piece_index>
```

### Example

```bash
./runner.sh magnet_download_piece -o /tmp/test-piece-0 "magnet:?xt=urn:btih:..." 0
```

---

## Step-by-Step Implementation (Go)

### 1. Parse Arguments

```go
case "magnet_download_piece":
    if len(os.Args) < 6 || os.Args[2] != "-o" {
        fmt.Fprintln(os.Stderr, "Usage: ./runner.sh magnet_download_piece -o <output> <magnet-link> <piece_index>")
        os.Exit(1)
    }

    outputPath := os.Args[3]
    magnetLink := os.Args[4]
    pieceIndex, err := strconv.Atoi(os.Args[5])
```

### 2. Parse Magnet Link & Get Peers (same as magnet_info)

Extract `infoHashHex`, `trackerUrl`, decode to raw `infoHash`, contact the tracker, and pick a peer.

### 3. Base Handshake + Extension Handshake (same as magnet_info)

Perform the full handshake sequence with extension bit set, receive bitfield, exchange extension handshakes, get `peerMetadataExtId`.

### 4. Request & Receive Metadata (same as magnet_info)

Send metadata request, receive the data response, parse to get `rawMetadata`, verify hash, decode the info dictionary to extract:

```go
totalLength := infoDict["length"].(int)
normalPieceLength := infoDict["piece length"].(int)
piecesStr := infoDict["pieces"].(string)
```

### 5. Calculate Piece Size

The last piece may be smaller than the normal piece length:

```go
pieceLength := normalPieceLength
totalPieces := (totalLength + normalPieceLength - 1) / normalPieceLength
if pieceIndex == totalPieces-1 {
    pieceLength = totalLength - (pieceIndex * normalPieceLength)
}
```

### 6. Send Interested & Wait for Unchoke

```go
// Send interested (message ID = 2)
interested := make([]byte, 5)
binary.BigEndian.PutUint32(interested[0:4], 1)
interested[4] = 2
_, err = conn.Write(interested)

// Wait for unchoke (message ID = 1)
for {
    _, err = io.ReadFull(conn, lengthBuf)
    msgLen = binary.BigEndian.Uint32(lengthBuf)
    if msgLen == 0 {
        continue // keepalive
    }
    msgBuf = make([]byte, msgLen)
    _, err = io.ReadFull(conn, msgBuf)
    if msgBuf[0] == 1 { // unchoke
        break
    }
}
```

### 7. Request All Blocks for the Piece

Each block is 16 KiB (16384 bytes), except possibly the last one:

```go
blockSize := 16384
totalBlocks := (pieceLength + blockSize - 1) / blockSize
pieceData := make([]byte, pieceLength)

for i := 0; i < totalBlocks; i++ {
    offset := i * blockSize
    length := blockSize
    if offset+length > pieceLength {
        length = pieceLength - offset
    }

    // Send request (message ID = 6)
    request := make([]byte, 17)
    binary.BigEndian.PutUint32(request[0:4], 13)
    request[4] = 6
    binary.BigEndian.PutUint32(request[5:9], uint32(pieceIndex))
    binary.BigEndian.PutUint32(request[9:13], uint32(offset))
    binary.BigEndian.PutUint32(request[13:17], uint32(length))
    _, err = conn.Write(request)
}
```

### 8. Receive All Blocks

```go
blocksReceived := 0
for blocksReceived < totalBlocks {
    _, err = io.ReadFull(conn, lengthBuf)
    msgLen = binary.BigEndian.Uint32(lengthBuf)
    if msgLen == 0 {
        continue
    }
    msgBuf = make([]byte, msgLen)
    _, err = io.ReadFull(conn, msgBuf)
    if msgBuf[0] != 7 { // not a piece message
        continue
    }
    begin := binary.BigEndian.Uint32(msgBuf[5:9])
    copy(pieceData[begin:], msgBuf[9:])
    blocksReceived++
}
```

### 9. Verify Piece Hash & Save to Disk

```go
expectedHash := piecesStr[pieceIndex*20 : (pieceIndex+1)*20]
actualHash := sha1.Sum(pieceData)
if string(actualHash[:]) != expectedHash {
    fmt.Fprintln(os.Stderr, "Error: piece hash mismatch")
    os.Exit(1)
}

err = os.WriteFile(outputPath, pieceData, 0644)
if err != nil {
    fmt.Fprintln(os.Stderr, "Error writing file:", err)
    os.Exit(1)
}
fmt.Fprintf(os.Stderr, "Piece %d downloaded to %s.\n", pieceIndex, outputPath)
```

---

## Key Differences from `download_piece`

| Aspect            | `download_piece`         | `magnet_download_piece`                     |
| ----------------- | ------------------------ | ------------------------------------------- |
| Input             | `.torrent` file path     | Magnet link                                 |
| Metadata source   | Read from file           | Fetched from peer via metadata extension    |
| Handshake         | Standard (no extensions) | Extension bit set, extension handshake done |
| Peer discovery    | Tracker from `.torrent`  | Tracker from magnet link `tr` parameter     |
| Block downloading | Same                     | Same                                        |

---

## Running the Command

```bash
./runner.sh magnet_download_piece -o /tmp/test-piece-0 "magnet:?xt=urn:btih:d69f91e6b2ae4c542468d1073a71d4ea13879a7f&dn=magnet3.gif&tr=http%3A%2F%2Fbittorrent-test-tracker.codecrafters.io%2Fannounce" 0
```
