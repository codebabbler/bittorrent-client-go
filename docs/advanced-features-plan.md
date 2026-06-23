# Implementation Plan: Multi-file Torrents, UDP Trackers, DHT & Seeding

## Overview

Four major features to evolve the client from a single-file downloader into a fuller BitTorrent client. Ordered by dependency and complexity — each phase builds on the previous.

---

## Phase 1 — Multi-file Torrents (BEP 3)

Currently `TorrentFile.Length` is a single int and `download.File` writes one blob. Multi-file torrents use a `files` list in the info dict instead of a single `length`.

### Info dict difference

```
# Single-file:
{"length": 92063, "name": "sample.gif", ...}

# Multi-file:
{"files": [{"length": 1024, "path": ["dir", "file1.txt"]},
           {"length": 2048, "path": ["file2.txt"]}],
 "name": "my-torrent", ...}
```

Pieces span across file boundaries — a single piece may contain bytes from multiple files.

### Proposed Changes

---

#### [MODIFY] [torrent.go](file:///home/mrz/projects/bittorrent-client-go/torrent/torrent.go)

- Add `FileEntry` struct:

```go
type FileEntry struct {
    Length int
    Path   []string // e.g. ["subdir", "file.txt"]
}
```

- Add `Files []FileEntry` field to `TorrentFile`
- Add `IsMultiFile bool` field
- In `ParseFile`: detect `files` key vs `length` key in info dict
  - Single-file: set `Length` as before, `Files = nil`, `IsMultiFile = false`
  - Multi-file: populate `Files` slice, compute `Length` as sum of all file lengths, set `IsMultiFile = true`
- Add `TotalLength() int` helper that works for both modes

#### [MODIFY] [magnet.go](file:///home/mrz/projects/bittorrent-client-go/torrent/magnet.go)

- Same changes in `ToTorrentFile` — detect multi-file info dict

#### [NEW] [filewriter.go](file:///home/mrz/projects/bittorrent-client-go/download/filewriter.go)

- `WriteFiles(outputDir string, tf *torrent.TorrentFile, data []byte) error`
  - Creates directory tree under `outputDir/tf.Name/`
  - Splits the flat byte buffer into per-file slices based on `FileEntry.Length` offsets
  - Writes each file to `outputDir/tf.Name/path[0]/path[1]/.../filename`

#### [MODIFY] [download.go](file:///home/mrz/projects/bittorrent-client-go/download/download.go)

- No changes to `Piece` or `File` — they work on flat piece data, which is correct
- The multi-file logic is in the writer, not the downloader

#### [MODIFY] [main.go](file:///home/mrz/projects/bittorrent-client-go/app/main.go)

- `download` and `magnet_download` cases: after `download.File()`, check `tf.IsMultiFile`
  - Single-file: `os.WriteFile(outputPath, ...)` (existing behavior)
  - Multi-file: `download.WriteFiles(outputDir, tf, data)`

---

## Phase 2 — UDP Trackers (BEP 15)

Currently `tracker.GetPeers` only supports HTTP (`http.Get`). Many trackers use `udp://` which uses a binary protocol over UDP.

### Protocol summary

1. **Connect**: send 16-byte connect request → receive 16-byte connect response with `connection_id`
2. **Announce**: send 98-byte announce request (with `connection_id`, `info_hash`, `peer_id`, `left`, etc.) → receive announce response with peers in compact 6-byte format
3. All messages use big-endian binary encoding and a `transaction_id` for matching req/resp

### Proposed Changes

---

#### [NEW] [udp.go](file:///home/mrz/projects/bittorrent-client-go/tracker/udp.go)

- `GetPeersUDP(trackerURL string, infoHash []byte, left int) ([]Peer, error)`
- Internal functions:
  - `connect(conn *net.UDPConn) (connectionID uint64, err error)` — send connect, recv connect response, validate `transaction_id`
  - `announce(conn *net.UDPConn, connID uint64, infoHash []byte, left int) ([]Peer, error)` — build 98-byte announce, recv response, parse compact peers
- Constants: `actionConnect = 0`, `actionAnnounce = 1`, `protocolMagic = 0x41727101980`
- Timeout/retry: 15s initial timeout, up to 3 retries (BEP 15 specifies `15 * 2^n` seconds)

#### [MODIFY] [tracker.go](file:///home/mrz/projects/bittorrent-client-go/tracker/tracker.go)

- Rename existing `GetPeers` → `getPeersHTTP` (unexported)
- New exported `GetPeers` that dispatches based on URL scheme:

```go
func GetPeers(trackerURL string, infoHash []byte, left int) ([]Peer, error) {
    u, _ := url.Parse(trackerURL)
    switch u.Scheme {
    case "http", "https":
        return getPeersHTTP(trackerURL, infoHash, left)
    case "udp":
        return GetPeersUDP(trackerURL, infoHash, left)
    default:
        return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
    }
}
```

- **No changes to `main.go`** — `tracker.GetPeers` signature is unchanged

---

## Phase 3 — DHT Peer Discovery (BEP 5)

DHT enables finding peers without any tracker. Uses a Kademlia-based distributed hash table over UDP (default port 6881).

### Key concepts

- **Node ID**: 20-byte random ID for our node
- **Routing table**: buckets of known nodes, organized by XOR distance from our ID
- **Krpc**: all DHT messages are bencoded dicts sent over UDP with keys `t` (transaction), `y` (type: `q`/`r`/`e`), `q` (query name), `a` (args), `r` (response)
- **Bootstrap**: contact a known node (e.g. `router.bittorrent.com:6881`), do `find_node` for our own ID to populate routing table
- **get_peers**: query nodes close to the target info hash, receive either peers or closer nodes, iterate until peers found

### Proposed Changes

---

#### [NEW] [dht/](file:///home/mrz/projects/bittorrent-client-go/dht/) package

#### [NEW] [dht/node.go](file:///home/mrz/projects/bittorrent-client-go/dht/node.go)

- `NodeID [20]byte`
- `Node` struct: `ID NodeID`, `Addr *net.UDPAddr`
- `XORDistance(a, b NodeID) NodeID`
- `GenerateNodeID() NodeID`

#### [NEW] [dht/routing.go](file:///home/mrz/projects/bittorrent-client-go/dht/routing.go)

- `RoutingTable` struct with 160 k-buckets (k=8 per BEP 5)
- `Add(node Node)`, `FindClosest(target NodeID, count int) []Node`
- Bucket splitting logic (simplified: don't split, just evict stale nodes)

#### [NEW] [dht/krpc.go](file:///home/mrz/projects/bittorrent-client-go/dht/krpc.go)

- `Message` struct: `TransactionID`, `Type` (`q`/`r`/`e`), `Query`, `Args`, `Response`
- `Encode(msg Message) []byte` — bencode the message
- `Decode(data []byte) (Message, error)` — parse response
- Transaction ID tracking for matching responses

#### [NEW] [dht/dht.go](file:///home/mrz/projects/bittorrent-client-go/dht/dht.go)

- `DHT` struct: `conn *net.UDPConn`, `nodeID NodeID`, `routingTable *RoutingTable`, `port int`
- `New(port int) (*DHT, error)` — bind UDP, generate node ID
- `Bootstrap(addresses []string) error` — `find_node` against bootstrap nodes
- `GetPeers(infoHash [20]byte) ([]tracker.Peer, error)` — iterative lookup:
  1. Find α=3 closest nodes to info_hash in routing table
  2. Send `get_peers` query to each
  3. If response has `values` (peers), collect them
  4. If response has `nodes` (closer nodes), add to routing table, continue
  5. Repeat until no closer nodes found or enough peers collected
- `Close()`

#### [NEW] [dht/bootstrap.go](file:///home/mrz/projects/bittorrent-client-go/dht/bootstrap.go)

- Default bootstrap nodes: `router.bittorrent.com:6881`, `dht.transmissionbt.com:6881`, `router.utorrent.com:6881`

#### [MODIFY] [main.go](file:///home/mrz/projects/bittorrent-client-go/app/main.go)

- New command `dht_peers <info_hash_hex>`:
  - Creates DHT instance, bootstraps, calls `GetPeers`, prints peers
- Optionally integrate into existing commands as fallback when tracker fails

---

## Phase 4 — Seeding

Currently the client is download-only. Seeding means serving pieces to other peers that request them.

### Key concepts

- **Listen** for incoming TCP connections on a port
- **Receive handshakes**, validate info hash, send handshake back
- **Send bitfield** indicating which pieces we have
- **Handle request messages**: read the requested block from disk, send piece message back
- **Choking/unchoking**: basic tit-for-tat — unchoke peers who upload to us or reciprocate

### Proposed Changes

---

#### [NEW] [seeder/](file:///home/mrz/projects/bittorrent-client-go/seeder/) package

#### [NEW] [seeder/server.go](file:///home/mrz/projects/bittorrent-client-go/seeder/server.go)

- `Server` struct: `listener net.Listener`, `infoHash [20]byte`, `pieces [][]byte` (in-memory piece data), `port int`
- `New(port int, tf *torrent.TorrentFile, dataPath string) (*Server, error)` — load file data, split into pieces, verify hashes
- `Start()` — `listener.Accept()` loop, spawns `handlePeer` goroutine per connection
- `Stop()`

#### [NEW] [seeder/handler.go](file:///home/mrz/projects/bittorrent-client-go/seeder/handler.go)

- `handlePeer(conn net.Conn)`:
  1. Receive handshake, validate info hash
  2. Send handshake back
  3. Send bitfield (all bits set since we have complete file)
  4. Wait for `interested` message
  5. Send `unchoke`
  6. Loop: read `request` messages, respond with `piece` messages
     - Extract `pieceIndex`, `begin`, `length` from request payload
     - Read block from `pieces[pieceIndex][begin:begin+length]`
     - Send back as piece message
  7. Handle `not interested` → choke the peer

#### [NEW] [seeder/storage.go](file:///home/mrz/projects/bittorrent-client-go/seeder/storage.go)

- `LoadPieces(dataPath string, tf *torrent.TorrentFile) ([][]byte, error)` — reads file(s), splits into piece-length chunks
- `BuildBitfield(numPieces int) []byte` — creates a bitfield with all bits set
- For multi-file torrents: reads all files in order, concatenates, then splits

#### [MODIFY] [message.go](file:///home/mrz/projects/bittorrent-client-go/peer/message.go)

- Add constants: `MsgNotInterested uint8 = 3`, `MsgHave uint8 = 4`, `MsgCancel uint8 = 8`
- Add `ParseRequest(payload []byte) (index, begin, length uint32)` helper

#### [MODIFY] [handshake.go](file:///home/mrz/projects/bittorrent-client-go/peer/handshake.go)

- Add `ReceiveHandshake(conn net.Conn) (infoHash [20]byte, peerID [20]byte, err error)` — for incoming connections where we receive first

#### [MODIFY] [main.go](file:///home/mrz/projects/bittorrent-client-go/app/main.go)

- New command `seed <torrent_file> <data_path>`:
  - Parse torrent, create server, register with tracker (`event=started`), start serving
  - Print listening address
  - Block until Ctrl+C

#### [MODIFY] [tracker.go](file:///home/mrz/projects/bittorrent-client-go/tracker/tracker.go)

- Add `Announce(trackerURL string, infoHash []byte, event string, port int, uploaded, downloaded, left int) ([]Peer, error)` — full announce with event support (`started`, `stopped`, `completed`)
- Existing `GetPeers` calls this with `event=""` for backward compatibility

---

## User Review Required

> [!IMPORTANT]
> **Phase ordering**: The phases are independent enough to implement in any order, but Phase 1 (multi-file) is the simplest and a good starting point. Phase 3 (DHT) is the most complex. Which phase(s) should we prioritize?

> [!WARNING]
> **Seeding security**: The seeding server will listen on a TCP port. In production this needs rate limiting and connection caps. The initial implementation will be basic (no choking algorithm, no peer limits).

> [!IMPORTANT]
> **DHT scope**: A full BEP 5 implementation is substantial (~500-800 lines). We could start with a minimal version that only does `get_peers` lookups using bootstrap nodes, without maintaining a persistent routing table. Should we go minimal or full?

---

## Verification Plan

### Automated Tests

Since there are no existing unit tests, we'll verify via build + CLI commands:

```bash
# Phase 1: Multi-file torrents
go build ./...
# Need a multi-file .torrent file for testing — user to provide or create one

# Phase 2: UDP trackers
go build ./...
# Test with a torrent that has a UDP tracker URL, e.g.:
# ./runner.sh peers <torrent-with-udp-tracker>

# Phase 3: DHT
go build ./...
# ./runner.sh dht_peers <info_hash_hex>

# Phase 4: Seeding
go build ./...
# Terminal 1: ./runner.sh seed sample.torrent ./downloaded-file
# Terminal 2: ./runner.sh download_piece -o /tmp/test-piece sample.torrent 0
#   (pointing at localhost:<seed_port>)
```

### Manual Verification

1. **Multi-file**: Download a well-known multi-file torrent (e.g. from a test tracker), verify directory structure and file contents match expected
2. **UDP tracker**: Use a torrent with `udp://` tracker URL, verify peers are returned
3. **DHT**: Run `dht_peers` with a popular info hash, verify peers returned without any tracker
4. **Seeding**: Run seed command, use another client (or our own) to download from it, verify data integrity

> [!NOTE]
> Since these features interact with external services (trackers, peers, DHT nodes), automated unit testing is limited. The primary verification is that `go build ./...` passes and the CLI commands produce correct output against real-world trackers/peers. User should submit to CodeCrafters or test manually against known torrents.
