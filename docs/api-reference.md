# BitTorrent Client — Complete API & Method Documentation

Comprehensive reference for every type, function, and method in the client. Organized by package in dependency order.

---

## Table of Contents

1. [bencode/](#1-bencode--bencoded-serializationdeserialization)
2. [torrent/](#2-torrent--torrent-metadata)
3. [tracker/](#3-tracker--tracker-communication)
4. [peer/](#4-peer--peer-protocol)
5. [download/](#5-download--download-orchestration)
6. [dht/](#6-dht--distributed-hash-table)
7. [seeder/](#7-seeder--piece-serving)
8. [app/main.go](#8-appmain--cli-dispatcher)

---

## 1. `bencode/` — Bencoded Serialization/Deserialization

**File:** `bencode/decode.go` | **Dependencies:** none (leaf package)

Bencode is the encoding format used by BitTorrent for `.torrent` files, tracker responses, and DHT messages. It supports four types: **strings**, **integers**, **lists**, and **dictionaries**.

| Bencode         | Go Type                  | Example          |
| --------------- | ------------------------ | ---------------- |
| `5:hello`       | `string`                 | `"hello"`        |
| `i42e`          | `int`                    | `42`             |
| `l5:helloi42ee` | `[]interface{}`          | `["hello", 42]`  |
| `d3:foo3:bare`  | `map[string]interface{}` | `{"foo": "bar"}` |

---

### `Decode(bencodedString string) (interface{}, error)`

**The main public entry point.** Decodes any bencoded value into a Go type.

```go
result, err := bencode.Decode("5:hello")
// result = "hello" (string)

result, err := bencode.Decode("li52ei42e3:fooe")
// result = [52, 42, "foo"] ([]interface{})
```

Internally initializes a position cursor at `0` and delegates to the private `decode()` dispatcher.

---

### `decode(input string, pos *int) (interface{}, error)`

**Internal dispatcher.** Examines the character at `input[*pos]` and routes to the appropriate decoder:

```go
switch char := input[*pos]; {
case unicode.IsDigit(rune(char)):   // "5:hello" → DecodeString
    return DecodeString(input, pos)
case char == 'i':                    // "i42e"   → DecodeInteger
    return DecodeInteger(input, pos)
case char == 'l':                    // "l...e"  → DecodeList
    return DecodeList(input, pos)
case char == 'd':                    // "d...e"  → DecodeDict
    dict, _, err := DecodeDict(input, pos)
    return dict, err
}
```

The `pos` pointer is advanced past the consumed bytes by each decoder.

---

### `DecodeString(input string, pos *int) (string, error)`

Decodes a length-prefixed string: `<length>:<data>`.

**Algorithm:**

1. Read digits until `:` → parse as integer `length`
2. Skip the `:`
3. Read exactly `length` bytes → the string value
4. Advance `pos` past the string

```go
// Input: "5:hello" at pos=0
// Step 1: digits "5", pos→1 (the ':')
// Step 2: skip ':', pos→2
// Step 3: read 5 bytes "hello", pos→7
```

---

### `DecodeInteger(input string, pos *int) (int, error)`

Decodes an integer: `i<number>e`. Supports negative numbers.

**Algorithm:**

1. Skip `i`
2. Read characters until `e` → parse with `strconv.Atoi`
3. Skip `e`

```go
// Input: "i-42e" at pos=0
// Skip 'i', read "-42", skip 'e' → -42
```

---

### `DecodeList(input string, pos *int) ([]interface{}, error)`

Decodes a list: `l<values>e`. Recursively decodes each element.

**Algorithm:**

1. Skip `l`
2. Loop: if current char is `e`, consume it and return. Otherwise call `decode()` recursively
3. Append each decoded value to the result slice

---

### `DecodeDict(input string, pos *int) (map[string]interface{}, map[string]string, error)`

Decodes a dictionary: `d<key><value>...e`. **Returns two maps:**

| Return                   | Purpose                                                |
| ------------------------ | ------------------------------------------------------ |
| `map[string]interface{}` | Parsed key→value pairs                                 |
| `map[string]string`      | Raw bencoded bytes per key (for info hash computation) |

The second map is critical: to compute the info hash, we need the **raw bencoded bytes** of the `info` key, not the parsed Go values. The raw bytes are captured by recording `startPos` before decoding each value:

```go
startPos := *pos
value, err := decode(input, pos)
rawValues[key] = input[startPos:*pos]  // raw bytes of this value
```

This is what makes `sha1.Sum([]byte(rawValues["info"]))` work correctly.

---

## 2. `torrent/` — Torrent Metadata

**Files:** `torrent/torrent.go`, `torrent/magnet.go` | **Depends on:** `bencode/`

---

### Types

#### `FileEntry`

```go
type FileEntry struct {
    Length int
    Path   []string // e.g. ["subdir", "file.txt"]
}
```

Represents a single file within a multi-file torrent. The `Path` is a list of directory components ending with the filename, as stored in the torrent's `files` list.

#### `TorrentFile`

```go
type TorrentFile struct {
    Announce    string        // tracker URL
    InfoHash    [20]byte      // SHA-1 of the raw bencoded info dict
    InfoHashHex string        // hex-encoded info hash
    PieceLength int           // bytes per piece (except possibly the last)
    Length      int           // total file size (sum of all files if multi-file)
    Name        string        // suggested filename or directory name
    Pieces      string        // concatenated raw 20-byte SHA-1 hashes
    Files       []FileEntry   // non-nil for multi-file torrents
    IsMultiFile bool
}
```

The central data structure. Populated either by `ParseFile` (from `.torrent`) or `ToTorrentFile` (from magnet metadata).

#### `MagnetLink`

```go
type MagnetLink struct {
    InfoHash    []byte
    InfoHashHex string
    Name        string
    TrackerURL  string
}
```

---

### `ParseFile(path string) (*TorrentFile, error)`

Reads a `.torrent` file from disk and parses it into a `TorrentFile`.

**Step-by-step:**

1. **Read file** — `os.ReadFile(path)` → raw bytes
2. **Validate** — first byte must be `d` (bencoded dict)
3. **Decode** — `bencode.DecodeDict` → parsed dict + raw values map
4. **Extract fields:**
   - `announce` → tracker URL
   - `info` → the info dictionary
   - `sha1.Sum(rawValues["info"])` → the 20-byte info hash
   - `info["pieces"]` → concatenated piece hashes
   - `info["piece length"]` → piece size
5. **Detect multi-file vs single-file:**
   ```go
   if filesList, ok := info["files"].([]interface{}); ok {
       // Multi-file: iterate filesList, extract length + path per entry
       tf.IsMultiFile = true
       tf.Length = sum of all file lengths
   } else {
       // Single-file: use info["length"]
       tf.Length = info["length"]
   }
   ```

---

### `(tf *TorrentFile) PieceHashes() []string`

Returns hex-encoded SHA-1 hashes by splitting `tf.Pieces` into 20-byte chunks:

```go
for i := 0; i < len(tf.Pieces); i += 20 {
    hashes = append(hashes, hex.EncodeToString([]byte(tf.Pieces[i:i+20])))
}
```

Used by the `info` command to display individual piece hashes.

---

### `ParseMagnet(uri string) (*MagnetLink, error)`

Parses a magnet URI like:  
`magnet:?xt=urn:btih:<hash>&dn=<name>&tr=<tracker>`

Extracts three query parameters:

- `xt` → strip `urn:btih:` prefix → hex decode → `InfoHash`
- `dn` → `Name`
- `tr` → `TrackerURL`

---

### `(m *MagnetLink) ToTorrentFile(rawMetadata []byte) (*TorrentFile, error)`

Converts metadata received via BEP 9 (extension protocol) into a `TorrentFile`.

**Key step:** verifies the received metadata by comparing `sha1.Sum(rawMetadata)` against `m.InfoHashHex`. This prevents a malicious peer from sending fake metadata. Then decodes the raw bytes as a bencoded dict and extracts the same fields as `ParseFile`.

---

## 3. `tracker/` — Tracker Communication

**Files:** `tracker/tracker.go`, `tracker/udp.go` | **Depends on:** `bencode/`

---

### Types

#### `Peer`

```go
type Peer struct {
    IP   net.IP
    Port uint16
}
```

Represents a peer returned by the tracker. The `Address()` method formats it as `ip:port`.

---

### `GetPeers(trackerURL string, infoHash []byte, left int) ([]Peer, error)`

**Router function.** Dispatches to the appropriate protocol based on URL scheme:

```go
switch u.Scheme {
case "http", "https": return getPeersHTTP(...)
case "udp":           return GetPeersUDP(...)
}
```

**Parameters:**

- `infoHash` — 20-byte raw info hash (not hex)
- `left` — bytes remaining to download (used by tracker for peer selection)

---

### `getPeersHTTP(trackerURL string, infoHash []byte, left int) ([]Peer, error)`

HTTP tracker announce per BEP 3.

**Request:** `GET <trackerURL>?info_hash=...&peer_id=...&port=6881&compact=1&left=...`

**Response parsing:**

1. Read body → decode as bencoded dict
2. Check for `failure reason` key
3. Extract `peers` key → **compact format**: 6 bytes per peer (4 IP + 2 port, big-endian)

```go
for i := 0; i+6 <= len(peersStr); i += 6 {
    ip := net.IP(peersStr[i : i+4])
    port := binary.BigEndian.Uint16([]byte(peersStr[i+4 : i+6]))
}
```

---

### `GetPeersUDP(trackerURL string, infoHash []byte, left int) ([]Peer, error)`

UDP tracker announce per BEP 15. A two-step binary protocol over UDP.

**Step 1 — Connect:**

```
Request (16 bytes):
  [0:8]   protocol_magic = 0x41727101980 (uint64, big-endian)
  [8:12]  action = 0 (connect)
  [12:16] transaction_id (random uint32)

Response (16 bytes):
  [0:4]   action = 0
  [4:8]   transaction_id (must match)
  [8:16]  connection_id (use in announce)
```

**Step 2 — Announce:**

```
Request (98 bytes):
  [0:8]   connection_id
  [8:12]  action = 1 (announce)
  [12:16] transaction_id
  [16:36] info_hash (20 bytes)
  [36:56] peer_id (20 bytes)
  [56:64] downloaded (uint64)
  [64:72] left (uint64)
  [72:80] uploaded (uint64)
  [80:84] event (0=none)
  [84:88] IP (0=default)
  [88:92] key (random)
  [92:96] num_want (0xFFFFFFFF = default)
  [96:98] port (6881)

Response (20+ bytes):
  [0:4]   action = 1
  [4:8]   transaction_id
  [8:12]  interval
  [12:16] leechers
  [16:20] seeders
  [20:]   peers (6 bytes each, same compact format as HTTP)
```

Retries up to 3 times with exponential backoff (`15s × 2^retry`).

---

## 4. `peer/` — Peer Protocol

**Files:** `handshake.go`, `message.go`, `extension.go`, `client.go` | **Depends on:** `bencode/`

---

### Message Constants

```go
MsgChoke         = 0   // Peer is choking us (no downloads allowed)
MsgUnchoke       = 1   // Peer allows us to download
MsgInterested    = 2   // We want to download from this peer
MsgNotInterested = 3   // We don't want to download
MsgHave          = 4   // Peer has a new piece
MsgBitfield      = 5   // Bitmap of pieces the peer has
MsgRequest       = 6   // Request a block: index + begin + length
MsgPiece         = 7   // Block data: index + begin + data
MsgCancel        = 8   // Cancel a pending request
MsgExtension     = 20  // Extension protocol (BEP 10)
```

---

### `DoHandshake(conn net.Conn, infoHash []byte, extensions bool) ([20]byte, error)`

Performs the BitTorrent handshake — a fixed 68-byte exchange:

```
Handshake message (68 bytes):
  [0]     = 19 (protocol string length)
  [1:20]  = "BitTorrent protocol"
  [20:28] = reserved bytes (8 bytes)
             If extensions=true: byte[25] = 0x10 (bit 20 = extension support)
  [28:48] = info_hash (20 bytes)
  [48:68] = peer_id (20 bytes, randomly generated)
```

After sending, reads 68 bytes back. Returns the remote peer's ID from bytes `[48:68]`.

---

### `ReceiveHandshake(conn net.Conn) ([20]byte, [20]byte, error)`

For **incoming** connections (seeding). Reads 68 bytes without sending first. Returns:

- `[20]byte` — the peer's info hash (from `[28:48]`)
- `[20]byte` — the peer's ID (from `[48:68]`)

Used by the seeder to validate that the peer wants the torrent we're serving.

---

### `ReadMessage(conn net.Conn) (id uint8, payload []byte, err error)`

Reads a length-prefixed message from the TCP connection:

```
Wire format:
  [0:4]  length (uint32, big-endian) — includes ID byte
  [4]    message ID
  [5:]   payload

Keepalive: length=0, returns (0, nil, nil)
```

The function uses `io.ReadFull` to guarantee complete reads.

---

### `WriteMessage(conn net.Conn, id uint8, payload []byte) error`

Writes a length-prefixed message. Constructs the buffer:

```go
msg := make([]byte, 4+1+len(payload))
binary.BigEndian.PutUint32(msg[0:4], uint32(1+len(payload))) // length
msg[4] = id                                                   // message ID
copy(msg[5:], payload)                                        // payload
```

---

### `ReadBitfield(conn net.Conn) error`

Reads and **discards** the bitfield message. The bitfield is the first message after the handshake, indicating which pieces the peer has. Our client currently doesn't use this information for piece selection.

---

### `ParseRequest(payload []byte) (index, begin, length uint32)`

Deserializes a `MsgRequest` payload (12 bytes) into three uint32 fields:

- `index` — piece index
- `begin` — byte offset within the piece
- `length` — number of bytes requested

---

### `DoExtensionHandshake(conn net.Conn) (int, error)`

Performs the BEP 10 extension handshake to negotiate `ut_metadata` support.

**We send:** `{m: {ut_metadata: 1}}` — announcing our extension ID as 1.

**We receive:** The peer's extension handshake dict, extract `m.ut_metadata` — the **peer's** extension ID that we'll use when sending metadata requests.

The bencoded payload is prepended with a `0` byte (extension handshake ID):

```go
extPayload := []byte("d1:md11:ut_metadatai1eee")
WriteMessage(conn, MsgExtension, append([]byte{0}, extPayload...))
//                                       ^--- ext msg id 0 = handshake
```

---

### `RequestMetadata(conn net.Conn, peerExtId int) ([]byte, error)`

Sends a BEP 9 metadata request and receives the raw info dict bytes.

**Request:** `{msg_type: 0, piece: 0}` prepended with the peer's extension ID.

**Response:** An extension message where `payload[0]` is our metadata extension ID, followed by a bencoded dict + raw metadata bytes. The function decodes the dict (to find where it ends), then slices the remaining bytes as raw metadata:

```go
pos := 0
_, _, err = bencode.DecodeDict(dictStr, &pos)
rawMetadata := payload[1+pos:]  // everything after the bencoded dict
```

---

### Types

#### `Client`

```go
type Client struct {
    Conn              net.Conn
    PeerID            [20]byte
    PeerMetadataExtId int
}
```

High-level wrapper around a peer TCP connection.

---

### `NewClient(address string, infoHash []byte, extensions bool) (*Client, error)`

**Factory function.** Establishes a complete peer connection in three steps:

1. `net.Dial("tcp", address)` — TCP connect
2. `DoHandshake(conn, infoHash, extensions)` — protocol handshake
3. `ReadBitfield(conn)` — consume the bitfield message

If any step fails, the connection is closed before returning.

---

### `(c *Client) SetupExtensions() error`

Calls `DoExtensionHandshake` and stores the peer's `ut_metadata` ID in `c.PeerMetadataExtId`.

---

### `(c *Client) FetchMetadata() ([]byte, error)`

Delegates to `RequestMetadata(c.Conn, c.PeerMetadataExtId)`.

---

### `(c *Client) SendInterested() error`

Sends `MsgInterested` (ID=2, no payload). Tells the peer we want to download.

---

### `(c *Client) WaitForUnchoke() error`

Blocks until the peer sends `MsgUnchoke` (ID=1). Skips keepalives and other messages.

---

### `(c *Client) DownloadPiece(pieceIndex, pieceLength int) ([]byte, error)`

Downloads a single piece by splitting it into 16 KiB blocks and using **pipelined requests** — all requests are sent first, then all responses are collected:

```go
blockSize := 16384  // 16 KiB

// Phase 1: Send ALL block requests
for i := 0; i < totalBlocks; i++ {
    payload := [pieceIndex(4) | offset(4) | length(4)]
    WriteMessage(conn, MsgRequest, payload)
}

// Phase 2: Receive ALL blocks
for blocksReceived < totalBlocks {
    id, payload := ReadMessage(conn)
    if id == MsgPiece {
        begin := payload[4:8]           // byte offset
        copy(pieceData[begin:], payload[8:])  // block data
        blocksReceived++
    }
}
```

This pipelining is efficient because the peer can start processing and sending blocks before we finish sending all requests.

---

### `(c *Client) Close()`

Closes the underlying TCP connection.

---

## 5. `download/` — Download Orchestration

**Files:** `download.go`, `concurrent.go`, `filewriter.go` | **Depends on:** `peer/`, `tracker/`, `torrent/`

---

### `Piece(client, pieces, pieceIndex, pieceLength) ([]byte, error)`

Downloads a single piece and **verifies its SHA-1 hash**:

```go
pieceData := client.DownloadPiece(pieceIndex, pieceLength)
expectedHash := pieces[pieceIndex*20 : (pieceIndex+1)*20]
actualHash   := sha1.Sum(pieceData)
if string(actualHash[:]) != expectedHash → error
```

The `pieces` string contains concatenated 20-byte hashes, so piece `i`'s hash is at offset `i*20`.

---

### `File(client, pieces, totalLength, normalPieceLength) ([]byte, error)`

**Sequential single-peer download.** Downloads all pieces in order from one peer:

```go
for i := 0; i < totalPieces; i++ {
    pieceLength := normalPieceLength
    if i == totalPieces-1 {
        pieceLength = totalLength - (i * normalPieceLength)  // last piece may be shorter
    }
    pieceData := Piece(client, pieces, i, pieceLength)
    fileData = append(fileData, pieceData...)
}
```

Used by `download_piece`, `magnet_download_piece`, and `magnet_download` commands.

---

### `ConcurrentFile(peers, infoHash, pieces, totalLength, pieceLength, maxWorkers) ([]byte, error)`

**Concurrent multi-peer download** using a goroutine worker pool.

**Architecture:**

```
                    ┌──────────────┐
                    │   Work Chan  │ ← pieceWork{Index, Length, Hash}
                    │  (buffered)  │
                    └──┬──┬──┬──┬─┘
                       │  │  │  │
              ┌────────┘  │  │  └────────┐
              ▼           ▼  ▼           ▼
         ┌─────────┐ ┌─────────┐ ┌─────────┐
         │Worker 0 │ │Worker 1 │ │Worker N │   (goroutines)
         │ peer[0] │ │ peer[1] │ │ peer[N] │
         └────┬────┘ └────┬────┘ └────┬────┘
              │           │           │
              ▼           ▼           ▼
         ┌──────────────────────────────────┐
         │          Result Channel          │
         │    pieceResult{Index, Data}      │
         └──────────────┬───────────────────┘
                        ▼
                  Assemble in order
```

**Each worker goroutine:**

1. Connects to its assigned peer (`peer.NewClient`)
2. Sends `Interested`, waits for `Unchoke`
3. Loops over `workCh`, downloading and verifying one piece per iteration
4. Sends verified results to `resultCh`

**Worker count** is `min(maxWorkers, len(peers), totalPieces)`.

**Thread safety:** The work channel provides lock-free distribution — whichever worker finishes first picks up the next piece. `sync.WaitGroup` ensures clean shutdown.

---

### `WriteFiles(outputDir string, tf *TorrentFile, data []byte) error`

Splits flat downloaded data into individual files for multi-file torrents.

**Algorithm:**

1. Base directory = `outputDir/tf.Name/`
2. For each `FileEntry` in `tf.Files`:
   - Build path: `baseDir + f.Path[0] + f.Path[1] + ... + filename`
   - Create parent directories with `os.MkdirAll`
   - Slice `data[offset:offset+f.Length]` → write to file
   - Advance offset

Pieces span file boundaries — a single piece may contain bytes from the end of one file and the start of the next. This function handles that correctly because it works on the flat concatenated data, not on individual pieces.

---

## 6. `dht/` — Distributed Hash Table

**Files:** `node.go`, `routing.go`, `krpc.go`, `dht.go` | **Depends on:** `bencode/`, `tracker/`

BEP 5 Kademlia-based DHT for trackerless peer discovery over UDP.

---

### Types

#### `NodeID` — `[20]byte`

A 160-bit identifier for DHT nodes. Same size as info hashes (by design — XOR distance between a node ID and an info hash determines which node stores peer info).

#### `Node`

```go
type Node struct {
    ID   NodeID
    Addr *net.UDPAddr
}
```

#### `RoutingTable`

```go
type RoutingTable struct {
    selfID  NodeID
    buckets [160][]Node  // 160 k-buckets, up to 8 nodes each
    mu      sync.RWMutex
}
```

#### `KRPCResponse`

```go
type KRPCResponse struct {
    TransactionID string
    Type          string   // "r" (response) or "e" (error)
    ID            NodeID   // responding node's ID
    Nodes         []Node   // compact node info from find_node/get_peers
    Values        []string // compact peer info from get_peers
    Token         string   // opaque token for announce_peer
    ErrorCode     int
    ErrorMsg      string
}
```

#### `DHT`

```go
type DHT struct {
    conn         *net.UDPConn
    nodeID       NodeID
    routingTable *RoutingTable
    port         int
}
```

---

### Node Functions (`node.go`)

#### `GenerateNodeID() (NodeID, error)`

Creates a cryptographically random 20-byte ID using `crypto/rand`.

#### `XORDistance(a, b NodeID) NodeID`

Byte-by-byte XOR of two node IDs. The result's bit pattern determines proximity in Kademlia — identical IDs have distance zero, maximally different IDs have distance `2^160 - 1`.

```go
for i := 0; i < 20; i++ {
    dist[i] = a[i] ^ b[i]
}
```

#### `CompareDistance(target, a, b NodeID) int`

Returns which of `a` or `b` is closer to `target` by comparing XOR distances byte-by-byte. Returns `-1` if `a` is closer, `+1` if `b` is closer, `0` if equal.

#### `CompactNodesEncode(nodes []Node) []byte`

Encodes nodes into compact format: 26 bytes per node (20 ID + 4 IP + 2 port).

#### `CompactNodesDecode(data []byte) []Node`

Decodes compact node info. Iterates in 26-byte steps, extracting ID, IP, and port.

---

### Routing Table (`routing.go`)

#### `NewRoutingTable(selfID NodeID) *RoutingTable`

Creates a routing table with 160 empty k-buckets. Buckets are indexed by the position of the first differing bit between our ID and the node's ID.

#### `(rt *RoutingTable) bucketIndex(id NodeID) int`

Finds the bucket index by counting leading zero bits in `XORDistance(selfID, id)`:

```go
dist := XORDistance(rt.selfID, id)
for i := 0; i < 20; i++ {
    for bit := 7; bit >= 0; bit-- {
        if dist[i]&(1<<uint(bit)) != 0 {
            return i*8 + (7 - bit)  // position of first '1' bit
        }
    }
}
```

Closer nodes (sharing more prefix bits) go in higher-numbered buckets.

#### `(rt *RoutingTable) Add(node Node)`

Inserts a node into its computed bucket:

- Skips if `node.ID == selfID`
- If already in bucket → moves to end (most recently seen)
- If bucket not full (< 8 nodes) → appends
- If full → discards (simplified; not implementing stale node eviction)

Thread-safe via `sync.Mutex`.

#### `(rt *RoutingTable) FindClosest(target NodeID, count int) []Node`

Collects all nodes from all buckets, sorts by XOR distance to `target`, and returns the `count` closest. Used in iterative lookups.

#### `(rt *RoutingTable) Count() int`

Returns total number of nodes across all buckets. Thread-safe via `sync.RWMutex`.

---

### KRPC Messages (`krpc.go`)

#### `encodeBencode(v interface{}) string`

Encodes Go values to bencode. Used internally for building DHT queries. Supports strings, ints, lists, and dicts. Dictionary keys are sorted (required by bencode spec).

#### `GenerateTransactionID() string`

Random integer string (0–65535) used to match requests with responses.

#### `BuildPingQuery(txnID, nodeID) []byte`

Builds: `{"t": txnID, "y": "q", "q": "ping", "a": {"id": nodeID}}`

#### `BuildFindNodeQuery(txnID, nodeID, target) []byte`

Builds: `{"t": txnID, "y": "q", "q": "find_node", "a": {"id": nodeID, "target": target}}`

Used during bootstrap to populate the routing table.

#### `BuildGetPeersQuery(txnID, nodeID, infoHash) []byte`

Builds: `{"t": txnID, "y": "q", "q": "get_peers", "a": {"id": nodeID, "info_hash": infoHash}}`

The core query for peer discovery.

#### `ParseKRPCResponse(data []byte) (*KRPCResponse, error)`

Decodes a bencoded KRPC response. Handles both success responses (`y=r`) and error responses (`y=e`). Extracts `nodes` (compact node info) and `values` (compact peer info) from the response dict.

---

### DHT Orchestration (`dht.go`)

#### `New(port int) (*DHT, error)`

Creates a DHT instance: binds a UDP socket, generates a random node ID, initializes an empty routing table.

#### `(d *DHT) Bootstrap(addresses []string) error`

Contacts known DHT nodes to populate the routing table.

**Algorithm:**

1. For each bootstrap address (default: `router.bittorrent.com:6881`, `dht.transmissionbt.com:6881`, `router.utorrent.com:6881`):
2. Send `find_node` for our own ID (this populates nearby nodes)
3. Parse response → add responding node + returned nodes to routing table
4. Fail if no nodes responded

#### `(d *DHT) GetPeers(infoHash [20]byte) ([]tracker.Peer, error)`

**Iterative Kademlia lookup.** Finds peers for a given info hash without any tracker.

**Algorithm (up to 20 iterations):**

1. Find α=3 closest known nodes to `infoHash` in routing table
2. For each unqueried node, send `get_peers`
3. Response contains either:
   - `values` → actual peers! Parse and return them
   - `nodes` → closer nodes. Add to routing table, continue iterating
4. Stop when peers found or no new closer nodes discovered

#### `parsePeerValues(data string) []tracker.Peer`

Parses compact peer info (6 bytes per peer: 4 IP + 2 port).

#### `(d *DHT) Close()`

Closes the UDP connection.

---

## 7. `seeder/` — Piece Serving

**Files:** `server.go`, `handler.go`, `storage.go` | **Depends on:** `peer/`, `torrent/`

---

### Storage (`storage.go`)

#### `LoadPieces(dataPath string, tf *TorrentFile) ([][]byte, error)`

Loads file data from disk and splits into piece-sized chunks with hash verification.

**For multi-file:** reads all files in order per `tf.Files`, concatenates into one buffer.  
**For single-file:** reads the single file.

Then splits by `tf.PieceLength` and verifies each piece's SHA-1 against `tf.Pieces`.

#### `BuildBitfield(numPieces int) []byte`

Creates a bitfield where **all bits are set** (we have every piece):

```go
for i := 0; i < numPieces; i++ {
    byteIndex := i / 8
    bitIndex  := 7 - (i % 8)
    bitfield[byteIndex] |= 1 << uint(bitIndex)
}
```

Bit 0 of byte 0 represents piece 0 (MSB first).

---

### Server (`server.go`)

#### `New(port int, tf *TorrentFile, dataPath string) (*Server, error)`

Creates a seeder: loads and verifies all pieces, starts TCP listener.

#### `(s *Server) Start()`

Accept loop — blocks indefinitely. Each accepted connection spawns a goroutine:

```go
for {
    conn := s.listener.Accept()
    go handlePeer(conn, s.infoHash, s.pieces, s.numPieces)
}
```

#### `(s *Server) Stop()` / `(s *Server) Port() int`

Close listener / return port number.

---

### Handler (`handler.go`)

#### `handlePeer(conn, infoHash, pieces, numPieces)`

Manages one peer connection for seeding. Full lifecycle:

1. **Receive handshake** → validate info hash matches ours
2. **Send handshake** back
3. **Send bitfield** (all pieces available)
4. **Message loop:**

| Incoming           | Action                                                                                                                 |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| `MsgInterested`    | Set `peerInterested=true`, send `MsgUnchoke`                                                                           |
| `MsgNotInterested` | Set `peerInterested=false`, send `MsgChoke`                                                                            |
| `MsgRequest`       | If interested: parse index/begin/length, read block from `pieces[index][begin:begin+length]`, send `MsgPiece` response |
| `MsgCancel`        | Ignored (simplified)                                                                                                   |

The piece response is built as:

```go
respPayload := [pieceIndex(4) | begin(4) | block(variable)]
WriteMessage(conn, MsgPiece, respPayload)
```

---

## 8. `app/main` — CLI Dispatcher

**File:** `app/main.go` | **Depends on:** all packages

The `main()` function is a `switch` on `os.Args[1]` dispatching to 14 commands:

| Command                 | Packages Used                                                                                    |
| ----------------------- | ------------------------------------------------------------------------------------------------ |
| `decode`                | `bencode.Decode` → `json.Marshal` → print                                                        |
| `info`                  | `torrent.ParseFile` → print metadata                                                             |
| `peers`                 | `torrent.ParseFile` → `tracker.GetPeers` → print addresses                                       |
| `handshake`             | `torrent.ParseFile` → `peer.NewClient` → print peer ID                                           |
| `download_piece`        | `torrent.ParseFile` → `tracker.GetPeers` → `peer.NewClient` → `download.Piece` → `os.WriteFile`  |
| `download`              | `torrent.ParseFile` → `tracker.GetPeers` → `download.ConcurrentFile` → write output              |
| `magnet_parse`          | `torrent.ParseMagnet` → print                                                                    |
| `magnet_handshake`      | `torrent.ParseMagnet` → `tracker.GetPeers` → `peer.NewClient` → `client.SetupExtensions` → print |
| `magnet_info`           | above + `client.FetchMetadata` → `magnet.ToTorrentFile` → print metadata                         |
| `magnet_download_piece` | above + `download.Piece` → `os.WriteFile`                                                        |
| `magnet_download`       | above + `download.File` → write output                                                           |
| `dht_peers`             | `dht.New` → `dht.Bootstrap` → `dht.GetPeers` → print peers                                       |
| `seed`                  | `torrent.ParseFile` → `seeder.New` → `srv.Start` (blocks)                                        |
