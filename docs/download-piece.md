# Downloading a Piece

## Overview

After completing the handshake with a peer, you can download file data by exchanging **peer messages**. Each piece is broken into 16 KiB blocks, requested individually, and reassembled. Once all blocks for a piece are received, you verify integrity using the SHA-1 hash from the torrent file, then save the piece to disk.

**Usage:**

```
./runner.sh download_piece -o /tmp/test-piece sample.torrent <piece_index>
```

---

## Peer Message Format

All messages after the handshake follow this structure:

| Field         | Size     | Description                                         |
| ------------- | -------- | --------------------------------------------------- |
| Length prefix | 4 bytes  | Big-endian `uint32`, length of message ID + payload |
| Message ID    | 1 byte   | Identifies the message type                         |
| Payload       | variable | Depends on the message type                         |

Messages with a length of **0** are **keepalives** and should be ignored.

### Relevant Message IDs

| ID  | Name       | Direction  | Payload                                    |
| --- | ---------- | ---------- | ------------------------------------------ |
| 1   | unchoke    | peer → you | empty                                      |
| 2   | interested | you → peer | empty                                      |
| 5   | bitfield   | peer → you | bitfield (ignore for now)                  |
| 6   | request    | you → peer | index (4B) + begin (4B) + length (4B)      |
| 7   | piece      | peer → you | index (4B) + begin (4B) + block (variable) |

---

## Download Flow

After the handshake is complete, follow these steps in order:

### 1. Receive Bitfield (message ID = 5)

The peer sends a **bitfield** message immediately after the handshake, indicating which pieces it has. Read the message but **ignore the payload** — the tracker guarantees all peers have all pieces.

```go
// Read 4-byte length prefix
lengthBuf := make([]byte, 4)
_, err = io.ReadFull(conn, lengthBuf)
messageLength := binary.BigEndian.Uint32(lengthBuf)

// Read the message (ID + payload)
messageBuf := make([]byte, messageLength)
_, err = io.ReadFull(conn, messageBuf)

// messageBuf[0] should be 5 (bitfield)
```

### 2. Send Interested (message ID = 2)

Tell the peer you want to download data. The payload is empty, so the length prefix is `1` (just the message ID byte).

```go
interested := make([]byte, 5)
binary.BigEndian.PutUint32(interested[0:4], 1) // length = 1
interested[4] = 2                               // message ID = interested
_, err = conn.Write(interested)
```

### 3. Wait for Unchoke (message ID = 1)

The peer will reply with an **unchoke** message when it's ready to serve data. Keep reading messages until you get one with ID = 1. Skip any keepalives (length = 0).

```go
for {
    _, err = io.ReadFull(conn, lengthBuf)
    messageLength = binary.BigEndian.Uint32(lengthBuf)

    if messageLength == 0 {
        continue // keepalive
    }

    messageBuf = make([]byte, messageLength)
    _, err = io.ReadFull(conn, messageBuf)

    if messageBuf[0] == 1 { // unchoke
        break
    }
}
```

### 4. Request Blocks (message ID = 6)

Each piece is broken into blocks of **16 KiB** (16 × 1024 = 16384 bytes). The last block may be smaller.

For each block, send a **request** message with this 12-byte payload:

| Field  | Size    | Description                                            |
| ------ | ------- | ------------------------------------------------------ |
| index  | 4 bytes | Zero-based piece index                                 |
| begin  | 4 bytes | Byte offset within the piece (0, 16384, 32768, ...)    |
| length | 4 bytes | Block size — 16384 for all but possibly the last block |

```go
blockSize := 16384 // 16 KiB

// Calculate this piece's actual size
pieceLength := // from torrent info["piece length"]
// For the LAST piece, it may be smaller:
//   totalLength - (pieceIndex * normalPieceLength)

for offset := 0; offset < pieceLength; offset += blockSize {
    length := blockSize
    if offset+length > pieceLength {
        length = pieceLength - offset
    }

    request := make([]byte, 17) // 4 (length prefix) + 1 (ID) + 12 (payload)
    binary.BigEndian.PutUint32(request[0:4], 13)            // length = 13
    request[4] = 6                                           // message ID = request
    binary.BigEndian.PutUint32(request[5:9], uint32(pieceIndex))
    binary.BigEndian.PutUint32(request[9:13], uint32(offset))
    binary.BigEndian.PutUint32(request[13:17], uint32(length))
    _, err = conn.Write(request)
}
```

### 5. Receive Piece Blocks (message ID = 7)

For each request you sent, read a **piece** message back. The payload is:

| Field | Size     | Description                  |
| ----- | -------- | ---------------------------- |
| index | 4 bytes  | Piece index                  |
| begin | 4 bytes  | Byte offset within the piece |
| block | variable | The actual data              |

```go
pieceData := make([]byte, pieceLength)
blocksReceived := 0
totalBlocks := (pieceLength + blockSize - 1) / blockSize // ceiling division

for blocksReceived < totalBlocks {
    // Read length prefix
    _, err = io.ReadFull(conn, lengthBuf)
    messageLength = binary.BigEndian.Uint32(lengthBuf)

    if messageLength == 0 {
        continue // keepalive
    }

    // Read message
    messageBuf = make([]byte, messageLength)
    _, err = io.ReadFull(conn, messageBuf)

    if messageBuf[0] != 7 { // not a piece message
        continue
    }

    // Parse payload
    index := binary.BigEndian.Uint32(messageBuf[1:5])
    begin := binary.BigEndian.Uint32(messageBuf[5:9])
    block := messageBuf[9:]

    // Copy block into the right position
    copy(pieceData[begin:], block)
    blocksReceived++
}
```

### 6. Verify Piece Integrity

Compare the SHA-1 hash of the downloaded piece against the hash from the torrent file:

```go
// Extract the expected hash from the pieces string
expectedHash := piecesStr[pieceIndex*20 : (pieceIndex+1)*20]

// Compute actual hash
actualHash := sha1.Sum(pieceData)

if string(actualHash[:]) != expectedHash {
    fmt.Fprintln(os.Stderr, "Error: piece hash mismatch")
    os.Exit(1)
}
```

### 7. Save to Disk

Write the verified piece data to the output file:

```go
err = os.WriteFile(outputPath, pieceData, 0644)
if err != nil {
    fmt.Fprintln(os.Stderr, "Error writing file:", err)
    os.Exit(1)
}

fmt.Fprintf(os.Stderr, "Piece %d downloaded to %s.\n", pieceIndex, outputPath)
```

---

## Calculating Piece Size

Most pieces are `info["piece length"]` bytes. The **last piece** is typically smaller:

```go
normalPieceLength := info["piece length"].(int)
totalLength := info["length"].(int)

pieceLength := normalPieceLength
if pieceIndex == (totalLength/normalPieceLength) {
    // Last piece — may be shorter
    pieceLength = totalLength - (pieceIndex * normalPieceLength)
}
```

---

## Quick Reference: Message Byte Layout

```
Request message (17 bytes total):
  [0:4]   length prefix = 13 (big-endian uint32)
  [4]     message ID = 6
  [5:9]   piece index (big-endian uint32)
  [9:13]  byte offset (big-endian uint32)
  [13:17] block length (big-endian uint32)

Piece message (variable length):
  [0:4]   length prefix (big-endian uint32)
  [4]     message ID = 7
  [5:9]   piece index (big-endian uint32)
  [9:13]  byte offset (big-endian uint32)
  [13:]   block data
```
