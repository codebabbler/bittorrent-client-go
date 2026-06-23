# Downloading an Entire File via Magnet Link (magnet_download)

## Overview

This command downloads **all pieces** of a file using a magnet link, concatenates them in order, and writes the complete file to disk. It combines the metadata exchange from `magnet_info` with the full-file download logic from `download`.

**Usage:**

```
./runner.sh magnet_download -o /tmp/sample <magnet-link>
```

---

## How It Builds on Previous Stages

| Stage                                 | Already done? |
| ------------------------------------- | ------------- |
| Parse magnet link                     | ✅            |
| Get peers from tracker                | ✅            |
| Base handshake with extension bit     | ✅            |
| Extension handshake                   | ✅            |
| Metadata request/receive              | ✅            |
| Download one piece via magnet         | ✅            |
| **Loop over all pieces and assemble** | 🔲 This stage |

---

## Flow

### 1. Metadata Exchange (same as magnet_download_piece)

Parse magnet → tracker → handshake → extension handshake → metadata request/receive → parse info dictionary to get `totalLength`, `normalPieceLength`, and `piecesStr`.

### 2. Interested & Unchoke (same as download)

Send interested (msg ID 2), wait for unchoke (msg ID 1).

### 3. Download Every Piece

Loop from piece `0` to `totalPieces - 1`:

```go
totalPieces := (totalLength + normalPieceLength - 1) / normalPieceLength
fileData := make([]byte, 0, totalLength)

for i := 0; i < totalPieces; i++ {
    pieceLength := normalPieceLength
    if i == totalPieces-1 {
        pieceLength = totalLength - (i * normalPieceLength)
    }
    // request blocks, receive, verify hash
    fileData = append(fileData, pieceData...)
}
```

### 4. Save to Disk

```go
os.WriteFile(outputPath, fileData, 0644)
```

---

## Key Differences from `download`

| `download`                | `magnet_download`                           |
| ------------------------- | ------------------------------------------- |
| Reads `.torrent` file     | Fetches metadata from peers via extension   |
| Standard handshake        | Handshake with extension bit + ext exchange |
| Metadata from file        | Metadata from BEP 9 data message            |
| Same block download logic | Same block download logic                   |

---

## Running the Command

```bash
./runner.sh magnet_download -o /tmp/sample "magnet:?xt=urn:btih:d69f91e6b2ae4c542468d1073a71d4ea13879a7f&dn=magnet3.gif&tr=http%3A%2F%2Fbittorrent-test-tracker.codecrafters.io%2Fannounce"
```
