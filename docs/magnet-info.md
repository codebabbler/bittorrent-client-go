# Requesting Torrent Metadata (magnet_info)

## Overview

A magnet link only contains the **info hash** and optionally a tracker URL. To get the full torrent metadata (file length, piece length, piece hashes), your client must **request it from peers** using the metadata extension ([BEP 9](https://www.bittorrent.org/beps/bep_0009.html)).

The metadata is the bencoded `info` dictionary — the same data you'd find inside a `.torrent` file. It's transferred in **16 KiB pieces** (for this challenge, assume the entire metadata fits in a single piece).

---

## Metadata Extension Message Types

| `msg_type` | Name    | Description                                    |
| ---------- | ------- | ---------------------------------------------- |
| `0`        | request | Request a piece of metadata from the peer      |
| `1`        | data    | Peer sends a piece of metadata back            |
| `2`        | reject  | Peer doesn't have the requested metadata piece |

> **Note:** These are metadata extension message types (`msg_type` inside the bencoded payload), not to be confused with base BitTorrent message IDs.

---

## The Metadata Request Message

This is a standard **extension message** (base message ID `20`), using the peer's `ut_metadata` extension ID:

| Field                 | Size     | Value                                                  |
| --------------------- | -------- | ------------------------------------------------------ |
| Message length prefix | 4 bytes  | Length of everything after this field                  |
| Message ID            | 1 byte   | `20` (extension message)                               |
| Extension message ID  | 1 byte   | **Peer's** `ut_metadata` ID (from extension handshake) |
| Bencoded dictionary   | variable | `{"msg_type": 0, "piece": 0}`                          |

### Key distinction

- **Your** `ut_metadata` ID → used by the peer when sending messages **to you**
- **Peer's** `ut_metadata` ID → used by you when sending messages **to the peer**

When sending a metadata request, use the **peer's** ID (the one you extracted from their extension handshake).

---

## Full Connection Flow

```
You                                  Peer
 │                                    │
 │──── Base Handshake ───────────────▶│
 │◀──── Base Handshake ──────────────│
 │                                    │
 │◀──── Bitfield (msg 5) ────────────│
 │                                    │
 │── Ext Handshake (msg 20, ext 0) ─▶│  {"m": {"ut_metadata": 1}}
 │◀── Ext Handshake (msg 20, ext 0) ─│  {"m": {"ut_metadata": <PEER_ID>}}
 │                                    │
 │── Metadata Request ──────────────▶│  msg 20, ext <PEER_ID>
 │   {"msg_type": 0, "piece": 0}      │  {"msg_type": 0, "piece": 0}
 │                                    │
 │◀── Metadata Data ─────────────────│  (later stage)
 │                                    │
```

---

## Step-by-Step Implementation (Go)

### 1. Build the Metadata Request Payload

Bencode the dictionary `{"msg_type": 0, "piece": 0}`:

```go
// d8:msg_typei0e5:piecei0ee
metadataReqPayload := []byte("d8:msg_typei0e5:piecei0ee")
```

### 2. Wrap in Extension Message Format

Use the **peer's** `ut_metadata` ID as the extension message ID:

```go
metadataReqMsg := make([]byte, 4+1+1+len(metadataReqPayload))
binary.BigEndian.PutUint32(metadataReqMsg[0:4], uint32(2+len(metadataReqPayload)))
metadataReqMsg[4] = 20                         // message ID: extension
metadataReqMsg[5] = byte(peerMetadataExtId)    // peer's ut_metadata ID
copy(metadataReqMsg[6:], metadataReqPayload)
```

### 3. Send the Request

```go
_, err = conn.Write(metadataReqMsg)
if err != nil {
    fmt.Fprintln(os.Stderr, "Error sending metadata request:", err)
    os.Exit(1)
}
```

### 4. Receive the Data Response (Later Stages)

The peer will respond with a `data` message (`msg_type: 1`). The response contains:

- A bencoded dictionary: `{"msg_type": 1, "piece": 0, "total_size": <size>}`
- Followed by the **raw metadata bytes** (appended after the bencoded dictionary, not inside it)

```go
// Read the response
_, err = io.ReadFull(conn, lengthBuf)
msgLen = binary.BigEndian.Uint32(lengthBuf)
msgBuf = make([]byte, msgLen)
_, err = io.ReadFull(conn, msgBuf)

// msgBuf[0] = 20 (extension)
// msgBuf[1] = your ut_metadata ID (e.g., 1)
// msgBuf[2:] = bencoded dict + raw metadata bytes
```

### 5. Parse the Metadata

The tricky part: the bencoded dictionary and raw metadata are **concatenated** in `msgBuf[2:]`. Decode the dictionary, then use the position to find where the raw metadata begins:

```go
dataDictStr := string(msgBuf[2:])
dataPos := 0
dataDict, _, err := decodeDict(dataDictStr, &dataPos)
// dataPos now points to the start of the raw metadata bytes
rawMetadata := msgBuf[2+dataPos:]
```

### 6. Parse the Raw Metadata as the Info Dictionary

The raw metadata is the bencoded `info` dictionary. Decode it to extract file length, piece length, and piece hashes:

```go
metadataStr := string(rawMetadata)
metaPos := 0
infoDict, _, err := decodeDict(metadataStr, &metaPos)

length := infoDict["length"].(int)
pieceLength := infoDict["piece length"].(int)
piecesStr := infoDict["pieces"].(string)
```

### 7. Verify the Info Hash

SHA-1 hash the raw metadata and verify it matches the info hash from the magnet link:

```go
computedHash := sha1.Sum(rawMetadata)
if hex.EncodeToString(computedHash[:]) != infoHashHex {
    fmt.Fprintln(os.Stderr, "Error: metadata hash mismatch")
    os.Exit(1)
}
```

### 8. Print the Results

```go
fmt.Println("Tracker URL:", trackerUrl)
fmt.Println("Length:", length)
fmt.Println("Info Hash:", infoHashHex)
fmt.Println("Piece Length:", pieceLength)
fmt.Println("Piece Hashes:")
for i := 0; i < len(piecesStr); i += 20 {
    end := i + 20
    if end > len(piecesStr) {
        end = len(piecesStr)
    }
    fmt.Println(hex.EncodeToString([]byte(piecesStr[i:end])))
}
```

---

## Running the Command

```bash
./runner.sh magnet_info "magnet:?xt=urn:btih:d69f91e6b2ae4c542468d1073a71d4ea13879a7f&dn=magnet3.gif&tr=http%3A%2F%2Fbittorrent-test-tracker.codecrafters.io%2Fannounce"
```

### Expected Output

```
Tracker URL: http://bittorrent-test-tracker.codecrafters.io/announce
Length: 92063
Info Hash: d69f91e6b2ae4c542468d1073a71d4ea13879a7f
Piece Length: 32768
Piece Hashes:
6e2275e604a0766656736e81ff10b55204ad8d35
e876f67a2a8886e8f36b136726c30fa29703022d
f00d937a0213df1982bc8d097227ad9e909acc17
```
