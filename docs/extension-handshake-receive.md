# Extension Handshake — Receiving the Peer's Response

## Overview

In the previous stage, your client sent an extension handshake message advertising `ut_metadata` support. Now you need to **parse the peer's extension handshake response** to learn the ID that the peer uses for the `ut_metadata` extension. This ID is needed in later stages to send metadata request messages.

---

## What the Peer Sends Back

The peer's extension handshake follows the same format your client sent:

| Field                 | Size     | Value                                              |
| --------------------- | -------- | -------------------------------------------------- |
| Message length prefix | 4 bytes  | Length of everything after this field              |
| Message ID            | 1 byte   | `20` (extension message)                           |
| Extension message ID  | 1 byte   | `0` (extension handshake)                          |
| Bencoded dictionary   | variable | Contains `"m"` with peer's extension name → ID map |

The bencoded payload will look something like:

```
d1:md11:ut_metadatai2ee13:metadata_sizei31235ee
```

Decoded:

```json
{
  "m": {
    "ut_metadata": 2
  },
  "metadata_size": 31235
}
```

- **`m.ut_metadata`** — the message ID the peer uses for metadata exchange. **This is what you need to extract and print.**
- **`metadata_size`** — the total size of the metadata in bytes (useful in later stages).

> **Important:** The peer's `ut_metadata` ID will likely differ from yours. Each peer chooses its own IDs independently.

---

## Extraction Steps (Go)

### 1. Parse the Extension Handshake Response

You already receive the raw message bytes. The bencoded dictionary starts at offset 2 (after the message ID and extension message ID):

```go
// msgBuf[0] = 20 (extension message ID)
// msgBuf[1] = 0  (extension handshake)
// msgBuf[2:] = bencoded dictionary

extDictStr := string(msgBuf[2:])
pos := 0
extDict, _, err := decodeDict(extDictStr, &pos)
if err != nil {
    fmt.Fprintln(os.Stderr, "Error decoding extension handshake:", err)
    os.Exit(1)
}
```

### 2. Extract the Peer's `ut_metadata` ID

Navigate into the `"m"` dictionary and get the `"ut_metadata"` value:

```go
mDict := extDict["m"].(map[string]interface{})
peerMetadataExtId := mDict["ut_metadata"].(int)
```

### 3. Print the Result

```go
fmt.Println("Peer Metadata Extension ID:", peerMetadataExtId)
```

---

## Updated Code for the Extension Handshake Section

Replace the extension handshake receive block with:

```go
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

// msgBuf[0] = 20, msgBuf[1] = 0, msgBuf[2:] = bencoded dict
extDictStr := string(msgBuf[2:])
pos := 0
extDict, _, err := decodeDict(extDictStr, &pos)
if err != nil {
    fmt.Fprintln(os.Stderr, "Error decoding extension handshake:", err)
    os.Exit(1)
}

mDict := extDict["m"].(map[string]interface{})
peerMetadataExtId := mDict["ut_metadata"].(int)
fmt.Println("Peer Metadata Extension ID:", peerMetadataExtId)
```

---

## Full Connection Flow Recap

```
You                              Peer
 │                                │
 │──── Base Handshake ───────────▶│
 │◀──── Base Handshake ──────────│
 │                                │
 │  Print: Peer ID: <hex>         │
 │                                │
 │◀──── Bitfield (msg 5) ────────│
 │                                │
 │── Ext Handshake (msg 20) ────▶│  {"m": {"ut_metadata": 1}}
 │◀── Ext Handshake (msg 20) ────│  {"m": {"ut_metadata": <ID>}}
 │                                │
 │  Print: Peer Metadata          │
 │         Extension ID: <ID>     │
```

---

## Running the Command

```bash
./runner.sh magnet_handshake "magnet:?xt=urn:btih:ad42ce8109f54c99613ce38f9b4d87e70f24a165&dn=magnet1.gif&tr=http%3A%2F%2Fbittorrent-test-tracker.codecrafters.io%2Fannounce"
```

### Expected Output

```
Peer ID: 0102030405060708090a0b0c0d0e0f1011121314
Peer Metadata Extension ID: 123
```

_(Both values are peer-dependent and will vary.)_
