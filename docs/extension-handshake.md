# Extension Handshake (Sending Supported Extensions)

## Overview

After completing the base peer handshake, if both peers support extensions (indicated by **bit 20** in the reserved bytes), they exchange **extension handshake messages** to communicate which extensions they support and what message IDs to use for each.

Each peer maintains its own mapping of extension names → IDs. The same extension may have different IDs on different peers, but the mapping stays stable for a given connection.

---

## Protocol Flow

The full connection flow with extension support looks like this:

```
You                          Peer
 │                            │
 │──── Base Handshake ───────▶│
 │◀──── Base Handshake ───────│
 │                            │
 │◀──── Bitfield (msg 5) ─────│  (receive and ignore)
 │                            │
 │  ┌─ Check reserved bit 20 in peer's handshake
 │  │  If set → peer supports extensions
 │  └─────────────────────────│
 │                            │
 │── Extension Handshake ────▶│  (msg ID 20, ext ID 0)
 │◀── Extension Handshake ────│  (contains peer's extension IDs)
 │                            │
```

> **Key:** The extension handshake is only sent if the peer's reserved bytes have bit 20 set. This maintains backward compatibility with older clients.

---

## Extension Message Format

All extension messages follow the standard BitTorrent message format:

| Field                 | Size     | Description                                           |
| --------------------- | -------- | ----------------------------------------------------- |
| Message length prefix | 4 bytes  | Total length of message ID + payload                  |
| Message ID            | 1 byte   | Always `20` for extension messages                    |
| Extension message ID  | 1 byte   | `0` for extension handshake, other IDs for extensions |
| Bencoded dictionary   | variable | The extension handshake payload                       |

### Extension Handshake Payload

The bencoded dictionary contains a key `"m"` whose value is another dictionary mapping extension names to message IDs:

```
{
  "m": {
    "ut_metadata": 1
  }
}
```

Bencoded: `d1:md11:ut_metadatai1eee`

- **`ut_metadata`** — the metadata exchange extension (BEP 9). The ID (`1` in this example) is the value **your** peer chooses. You can pick any non-zero value.

---

## Step-by-Step Implementation (Go)

### 1. Check if Peer Supports Extensions

After receiving the peer's base handshake, check bit 20 in the reserved bytes (byte offset 25 in the response):

```go
peerSupportsExtensions := response[25] & 0x10 != 0
```

### 2. Receive the Bitfield Message

Before sending the extension handshake, you'll receive a bitfield message (message ID 5):

```go
lengthBuf := make([]byte, 4)
_, err = io.ReadFull(conn, lengthBuf)
msgLen := binary.BigEndian.Uint32(lengthBuf)
if msgLen > 0 {
    msgBuf := make([]byte, msgLen)
    _, err = io.ReadFull(conn, msgBuf)
    // msgBuf[0] should be 5 (bitfield), ignore payload
}
```

### 3. Build the Extension Handshake Payload

Bencode a dictionary with a `"m"` key containing your supported extensions:

```go
// {"m": {"ut_metadata": 1}} bencoded
extHandshakePayload := []byte("d1:md11:ut_metadatai1eee")
```

You can also use your existing bencode encoder if you have one, but for a single static dictionary, a hardcoded string works fine.

### 4. Send the Extension Handshake Message

Wrap the payload in the standard message format:

```go
// Extension message: message ID = 20, extension message ID = 0 (handshake)
extMsg := make([]byte, 4+1+1+len(extHandshakePayload))
binary.BigEndian.PutUint32(extMsg[0:4], uint32(2+len(extHandshakePayload))) // length = msg ID + ext ID + payload
extMsg[4] = 20 // message ID: extension
extMsg[5] = 0  // extension message ID: handshake
copy(extMsg[6:], extHandshakePayload)

_, err = conn.Write(extMsg)
if err != nil {
    fmt.Fprintln(os.Stderr, "Error sending extension handshake:", err)
    os.Exit(1)
}
```

### 5. Receive the Peer's Extension Handshake

Read the peer's response (the next extension message with ID 20 and ext ID 0):

```go
// Read message length
_, err = io.ReadFull(conn, lengthBuf)
msgLen = binary.BigEndian.Uint32(lengthBuf)
msgBuf := make([]byte, msgLen)
_, err = io.ReadFull(conn, msgBuf)

// msgBuf[0] = 20 (extension message)
// msgBuf[1] = 0  (extension handshake)
// msgBuf[2:] = bencoded dictionary with peer's extension IDs
```

> **Note:** You may receive other messages before the extension handshake. Loop and skip messages until you get one with message ID `20` and extension ID `0`.

---

## Complete Updated `magnet_handshake` Command

Add this after receiving the base handshake and printing the peer ID:

```go
// Receive bitfield
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
}

// Check if peer supports extensions
if response[25]&0x10 != 0 {
    // Send extension handshake: {"m": {"ut_metadata": 1}}
    extPayload := []byte("d1:md11:ut_metadatai1eee")
    extMsg := make([]byte, 4+1+1+len(extPayload))
    binary.BigEndian.PutUint32(extMsg[0:4], uint32(2+len(extPayload)))
    extMsg[4] = 20 // message ID: extension
    extMsg[5] = 0  // extension message ID: handshake
    copy(extMsg[6:], extPayload)

    _, err = conn.Write(extMsg)
    if err != nil {
        fmt.Fprintln(os.Stderr, "Error sending extension handshake:", err)
        os.Exit(1)
    }

    // Receive extension handshake response
    _, err = io.ReadFull(conn, lengthBuf)
    if err != nil {
        fmt.Fprintln(os.Stderr, "Error:", err)
        os.Exit(1)
    }
    msgLen = binary.BigEndian.Uint32(lengthBuf)
    msgBuf := make([]byte, msgLen)
    _, err = io.ReadFull(conn, msgBuf)
    if err != nil {
        fmt.Fprintln(os.Stderr, "Error:", err)
        os.Exit(1)
    }
    // msgBuf[0] = 20, msgBuf[1] = 0, msgBuf[2:] = bencoded extension dict
}
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

---

## Quick Reference: Message Byte Layout

```
Extension handshake sent by your client:

Offset 0–3:   message length (big-endian uint32)
Offset 4:     20 (message ID for extension messages)
Offset 5:     0  (extension handshake ID)
Offset 6+:    bencoded dict  →  d1:md11:ut_metadatai1eee
```
