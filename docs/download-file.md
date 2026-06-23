# Downloading an Entire File

## Overview

To download a complete file, download **every piece** from a peer, verify each piece's SHA-1 hash, then concatenate all pieces in order and write the result to disk.

**Usage:**

```
./runner.sh download -o /tmp/test.txt sample.torrent
```

---

## How It Builds on Previous Stages

| Stage                                 | Already done? |
| ------------------------------------- | ------------- |
| Parse torrent file                    | ✅            |
| Get peers from tracker                | ✅            |
| TCP connect + handshake               | ✅            |
| Bitfield → interested → unchoke       | ✅            |
| Request/receive blocks for one piece  | ✅            |
| **Loop over all pieces and assemble** | 🔲 This stage |

The only new logic is wrapping the single-piece download in a loop and writing the combined output.

---

## Flow

### 1. Parse Torrent & Connect (same as before)

Parse the torrent file, contact the tracker, pick a peer, perform the handshake, and exchange bitfield/interested/unchoke messages. This is identical to `download_piece`.

### 2. Calculate the Number of Pieces

```go
totalPieces := (totalLength + normalPieceLength - 1) / normalPieceLength
```

### 3. Download Every Piece

Loop from piece `0` to `totalPieces - 1`. For each piece:

1. **Calculate piece size** — all pieces are `normalPieceLength` except the last, which may be smaller:

   ```go
   pieceLength := normalPieceLength
   if i == totalPieces-1 {
       pieceLength = totalLength - (i * normalPieceLength)
   }
   ```

2. **Break into 16 KiB blocks and send requests** (message ID = 6)

3. **Receive all blocks** (message ID = 7) and reassemble into `pieceData`

4. **Verify the SHA-1 hash** against `piecesStr[i*20 : (i+1)*20]`

5. **Append** the verified piece data to the full file buffer

```go
fileData := make([]byte, 0, totalLength)

for i := 0; i < totalPieces; i++ {
    // calculate pieceLength for this piece
    // request all blocks, receive them, reassemble into pieceData
    // verify hash

    fileData = append(fileData, pieceData...)
}
```

### 4. Save to Disk

Write the complete file in one call:

```go
err = os.WriteFile(outputPath, fileData, 0644)
```

---

## Complete `download` Command Example

```go
case "download":
    if len(os.Args) < 5 || os.Args[2] != "-o" {
        fmt.Fprintln(os.Stderr, "Usage: ./runner.sh download -o <output_file> <torrent_file>")
        os.Exit(1)
    }

    outputPath := os.Args[3]
    torrentFile := os.Args[4]

    // Parse torrent file (same as download_piece)
    // Get peers from tracker (same as download_piece)
    // TCP connect + handshake (same as download_piece)
    // Receive bitfield, send interested, wait for unchoke (same as download_piece)

    // Download all pieces
    totalPieces := (totalLength + normalPieceLength - 1) / normalPieceLength
    fileData := make([]byte, 0, totalLength)

    for i := 0; i < totalPieces; i++ {
        pieceLength := normalPieceLength
        if i == totalPieces-1 {
            pieceLength = totalLength - (i * normalPieceLength)
        }

        blockSize := 16384
        totalBlocks := (pieceLength + blockSize - 1) / blockSize
        pieceData := make([]byte, pieceLength)

        // Send all block requests
        for j := 0; j < totalBlocks; j++ {
            offset := j * blockSize
            length := blockSize
            if offset+length > pieceLength {
                length = pieceLength - offset
            }

            request := make([]byte, 17)
            binary.BigEndian.PutUint32(request[0:4], 13)
            request[4] = 6
            binary.BigEndian.PutUint32(request[5:9], uint32(i))
            binary.BigEndian.PutUint32(request[9:13], uint32(offset))
            binary.BigEndian.PutUint32(request[13:17], uint32(length))
            conn.Write(request)
        }

        // Receive all blocks
        blocksReceived := 0
        for blocksReceived < totalBlocks {
            io.ReadFull(conn, lengthBuf)
            msgLen := binary.BigEndian.Uint32(lengthBuf)
            if msgLen == 0 {
                continue
            }
            msgBuf := make([]byte, msgLen)
            io.ReadFull(conn, msgBuf)
            if msgBuf[0] != 7 {
                continue
            }
            begin := binary.BigEndian.Uint32(msgBuf[5:9])
            copy(pieceData[begin:], msgBuf[9:])
            blocksReceived++
        }

        // Verify hash
        expectedHash := piecesStr[i*20 : (i+1)*20]
        actualHash := sha1.Sum(pieceData)
        if string(actualHash[:]) != expectedHash {
            fmt.Fprintf(os.Stderr, "Error: hash mismatch for piece %d\n", i)
            os.Exit(1)
        }

        fileData = append(fileData, pieceData...)
        fmt.Fprintf(os.Stderr, "Piece %d/%d downloaded and verified.\n", i+1, totalPieces)
    }

    // Save complete file
    os.WriteFile(outputPath, fileData, 0644)
    fmt.Fprintf(os.Stderr, "Downloaded %s to %s.\n", torrentFile, outputPath)
```

---

## Key Differences from `download_piece`

| `download_piece`              | `download`                                 |
| ----------------------------- | ------------------------------------------ |
| Downloads one piece by index  | Downloads all pieces in a loop             |
| Writes single piece to output | Concatenates all pieces, writes full file  |
| Piece index from CLI arg      | Iterates `0` to `totalPieces - 1`          |
| Same connection setup         | Same connection setup (reuses single peer) |
