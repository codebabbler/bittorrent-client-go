# Magnet Handshake (Extension Support)

## Overview

Exchanging torrent metadata between peers wasn't part of the original BitTorrent protocol — it was added via the **extension protocol** ([BEP 10](https://www.bittorrent.org/beps/bep_0010.html)). To use extensions like the metadata exchange ([BEP 9](https://www.bittorrent.org/beps/bep_0009.html)), a client must signal support during the **peer handshake** by setting a specific bit in the reserved bytes.

---

## The Reserved Bytes

Recall the 68-byte handshake message format:

| Offset | Size     | Field                  |
| ------ | -------- | ---------------------- |
| 0      | 1 byte   | Protocol string length |
| 1–19   | 19 bytes | `BitTorrent protocol`  |
| 20–27  | 8 bytes  | **Reserved bytes**     |
| 28–47  | 20 bytes | Info hash              |
| 48–67  | 20 bytes | Peer ID                |

Previously, all 8 reserved bytes were set to `0x00`. To signal **extension protocol support**, set the **20th bit from the right** (0-indexed) to `1`.

### Bit Layout

```
Byte index (left to right):  [0]  [1]  [2]  [3]  [4]  [5]  [6]  [7]
Hex values:                   00   00   00   00   00   10   00   00
```

Byte 5 (`0x10`) in binary is `00010000` — that's the 20th bit from the right across all 64 bits:

```
... 00000000 00010000 00000000 00000000
                  ^ bit 20 (counting from the right, starting at 0)
```

---

## What Changes from the Previous Handshake

The **only** change to the handshake message is setting byte 5 (offset 25) of the reserved bytes to `0x10`:

```diff
 // reserved bytes [20:28] are already zero
+handshake[25] = 0x10  // set bit 20 to signal extension support
```

Everything else — protocol string, info hash, peer ID — stays the same.

---

## Step-by-Step Implementation (Go)

The `magnet_handshake` command combines the magnet link parsing with the peer handshake, using the info hash from the magnet link instead of a `.torrent` file.

### 1. Parse the Magnet Link

Extract the info hash and tracker URL (same as `magnet_parse`):

```go
magnetLink := os.Args[2]

parsedUrl, err := url.Parse(magnetLink)
if err != nil {
    fmt.Fprintln(os.Stderr, "Error parsing magnet link:", err)
    os.Exit(1)
}

xt := parsedUrl.Query().Get("xt")
infoHashHex := xt[len("urn:btih:"):]
trackerUrl := parsedUrl.Query().Get("tr")
```

### 2. Decode the Hex Info Hash to Raw Bytes

The magnet link provides the info hash as a 40-char hex string. For the handshake and tracker request, you need the raw 20-byte form:

```go
infoHash, err := hex.DecodeString(infoHashHex)
if err != nil {
    fmt.Fprintln(os.Stderr, "Error decoding info hash:", err)
    os.Exit(1)
}
```

### 3. Get Peers from the Tracker

Use the existing `getRequestToTracker` function. Since we don't know the file length from a magnet link, pass `0` for `left`:

```go
resp, err := getRequestToTracker(trackerUrl, string(infoHash), "-MT1230-rT6yUi8OpLkJ", 6881, 0, 0, 0)
```

Parse the response to extract peer addresses (same as in `peers` command).

### 4. Build the Handshake with Extension Bit

```go
var handshake [68]byte
handshake[0] = 19
copy(handshake[1:20], []byte("BitTorrent protocol"))

// Set the extension support bit (bit 20 from the right)
handshake[25] = 0x10

copy(handshake[28:48], infoHash)

var peerId [20]byte
_, err = rand.Read(peerId[:])
if err != nil {
    fmt.Fprintln(os.Stderr, "Error generating peer ID:", err)
    os.Exit(1)
}
copy(handshake[48:68], peerId[:])
```

### 5. Send & Receive the Handshake, Print Peer ID

```go
_, err = conn.Write(handshake[:])
// ...

var response [68]byte
_, err = io.ReadFull(conn, response[:])
// ...

receivedPeerId := response[48:68]
fmt.Println("Peer ID:", hex.EncodeToString(receivedPeerId))
```

---

## Complete `magnet_handshake` Command Example

```go
case "magnet_handshake":
    if len(os.Args) < 3 {
        fmt.Fprintln(os.Stderr, "Usage: ./runner.sh magnet_handshake <magnet-link>")
        os.Exit(1)
    }

    magnetLink := os.Args[2]

    // Parse magnet link
    parsedUrl, err := url.Parse(magnetLink)
    if err != nil {
        fmt.Fprintln(os.Stderr, "Error parsing magnet link:", err)
        os.Exit(1)
    }

    xt := parsedUrl.Query().Get("xt")
    infoHashHex := xt[len("urn:btih:"):]
    trackerUrl := parsedUrl.Query().Get("tr")

    // Decode hex info hash to raw bytes
    infoHash, err := hex.DecodeString(infoHashHex)
    if err != nil {
        fmt.Fprintln(os.Stderr, "Error decoding info hash:", err)
        os.Exit(1)
    }

    // Get peers from tracker
    resp, err := getRequestToTracker(trackerUrl, string(infoHash), "-MT1230-rT6yUi8OpLkJ", 6881, 0, 0, 0)
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

    // TCP connection
    conn, err := net.Dial("tcp", peerAddress)
    if err != nil {
        fmt.Fprintln(os.Stderr, "Error connecting:", err)
        os.Exit(1)
    }
    defer conn.Close()

    // Build handshake with extension support
    var handshake [68]byte
    handshake[0] = 19
    copy(handshake[1:20], []byte("BitTorrent protocol"))
    handshake[25] = 0x10 // extension support bit
    copy(handshake[28:48], infoHash)

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

    // Print peer ID
    receivedPeerId := response[48:68]
    fmt.Println("Peer ID:", hex.EncodeToString(receivedPeerId))
```

---

## Running the Command

```bash
./runner.sh magnet_handshake "magnet:?xt=urn:btih:ad42ce8109f54c99613ce38f9b4d87e70f24a165&dn=magnet1.gif&tr=http%3A%2F%2Fbittorrent-test-tracker.codecrafters.io%2Fannounce"
```

### Expected Output

```
Peer ID: 0102030405060708090a0b0c0d0e0f1011121314
```

_(Actual hex value depends on the peer.)_

---

## Why Bit 20?

The 8 reserved bytes form a 64-bit field. Each bit can flag support for a specific protocol extension. Bit 20 (from the right, 0-indexed) is assigned to the **extension protocol** (BEP 10). When a peer sees this bit set, it knows the client can exchange extension handshakes and messages — enabling features like metadata transfer (BEP 9), peer exchange (PEX), and more.
