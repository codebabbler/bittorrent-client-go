# Project Structure & Separation of Concerns

## Current State

Everything lives in a single `app/main.go` (~1970 lines) with all logic inlined inside `switch` cases. This document proposes a Go-idiomatic package layout that separates concerns.

### Implemented Commands

| Command                 | Description                                           |
| ----------------------- | ----------------------------------------------------- |
| `decode`                | Decode a bencoded value and print as JSON             |
| `info`                  | Parse a `.torrent` file and print torrent metadata    |
| `peers`                 | Get peer list from the tracker                        |
| `handshake`             | TCP handshake with a peer                             |
| `download_piece`        | Download a single piece from a `.torrent` file        |
| `download`              | Download an entire file from a `.torrent` file        |
| `magnet_parse`          | Parse a magnet link and print info hash + tracker URL |
| `magnet_handshake`      | Handshake with extension support via magnet link      |
| `magnet_info`           | Fetch torrent metadata from peers via BEP 9           |
| `magnet_download_piece` | Download a single piece via magnet link               |
| `magnet_download`       | Download an entire file via magnet link               |

---

## Proposed Package Layout

```
bittorrent-client-go/
├── app/
│   └── main.go              # CLI entry point only — parses args, dispatches commands
├── bencode/
│   ├── decode.go             # Decode functions (string, integer, list, dict)
│   └── encode.go             # Encode functions (for tracker requests)
├── torrent/
│   ├── torrent.go            # TorrentFile struct, ParseFile(), InfoHash(), PieceHashes()
│   └── magnet.go             # ParseMagnet(), magnet link handling
├── tracker/
│   ├── tracker.go            # GetPeers() — HTTP tracker requests & response parsing
│   └── udp.go                # [FUTURE] UDP tracker support
├── peer/
│   ├── peer.go               # Peer struct, address parsing from compact format
│   ├── handshake.go          # Handshake message construction & exchange
│   ├── message.go            # PeerMessage struct, Read/Write, message ID constants
│   ├── extension.go          # Extension handshake, BEP 9 metadata exchange
│   └── client.go             # PeerClient — manages connection state & piece downloading
├── download/
│   └── download.go           # Orchestrates downloading pieces/files using peer clients
├── dht/
│   └── dht.go                # [FUTURE] DHT peer discovery (BEP 5)
├── docs/
│   ├── peer-handshake.md
│   ├── download-piece.md
│   ├── download-file.md
│   ├── magnet-parse.md
│   ├── magnet-handshake.md
│   ├── extension-handshake.md
│   ├── extension-handshake-receive.md
│   ├── magnet-info.md
│   ├── magnet-download-piece.md
│   ├── magnet-download.md
│   └── project-structure.md  # This file
├── go.mod
├── go.sum
├── runner.sh
└── sample.torrent
```

---

## Package Responsibilities

### `app/` — CLI entry point

- Parses command-line arguments and flags
- Dispatches to the appropriate function based on the command
- **No business logic** — only arg validation and printing output

### `bencode/` — Bencode serialization

- `Decode(input string) (interface{}, error)` — public entry point
- `DecodeDict(input string) (map, rawMap, error)` — needed for info hash computation
- Encoding support for tracker request params
- **No dependencies** on other project packages

### `torrent/` — Torrent metadata

- `TorrentFile` struct with fields: `Announce`, `InfoHash`, `PieceHashes`, `PieceLength`, `Length`, `Name`
- `ParseFile(path string) (TorrentFile, error)` — reads `.torrent`, decodes, computes info hash
- `ParseMagnet(uri string) (MagnetLink, error)` — parses magnet URI into info hash + tracker URLs
- Depends on: `bencode/`

### `tracker/` — Tracker communication

- `GetPeers(tf TorrentFile, peerId [20]byte, port int) ([]peer.Peer, error)`
- Builds the tracker URL, makes the GET request, parses the compact peer list
- **Future:** UDP tracker support (BEP 15)
- Depends on: `bencode/`, `peer/` (for the `Peer` type)

### `peer/` — Peer protocol

- **`peer.go`** — `Peer` struct (`IP`, `Port`), `UnmarshalPeers(data string) []Peer`
- **`handshake.go`** — `Handshake` struct, `NewHandshake()`, `Send()`, `Read()`, extension bit support
- **`message.go`** — `Message` struct (`ID`, `Payload`), constants, `ReadMessage(conn)`, `WriteMessage(conn)`
- **`extension.go`** — Extension handshake (`ut_metadata` negotiation), BEP 9 metadata request/data/reject messages
- **`client.go`** — `Client` struct wrapping a `net.Conn` with:
  - `New(peer Peer, infoHash, peerId [20]byte) (*Client, error)` — connect + handshake
  - `SendInterested()`, `ReadBitfield()`, `waitForUnchoke()`
  - `RequestPiece(index, pieceLength int) ([]byte, error)` — block splitting, requesting, assembly
  - `FetchMetadata()` — BEP 9 metadata exchange for magnet links

### `download/` — Download orchestration

- `DownloadPiece(tf TorrentFile, peers []Peer, pieceIndex int) ([]byte, error)`
- `DownloadFile(tf TorrentFile, peers []Peer) ([]byte, error)` — loops over pieces, verifies hashes, concatenates
- Future: concurrent downloads from multiple peers
- Depends on: `torrent/`, `peer/`

### `dht/` — Distributed Hash Table (Future)

- DHT peer discovery (BEP 5) — find peers without a tracker
- Depends on: `peer/`

---

## Data Flow

```
CLI (app/main.go)
  │
  ├─ decode ────────────────► bencode.Decode()
  │
  ├─ info ──────────────────► torrent.ParseFile() ──► bencode
  │
  ├─ peers ─────────────────► torrent.ParseFile()
  │                            tracker.GetPeers() ──► bencode (response parsing)
  │
  ├─ handshake ─────────────► torrent.ParseFile()
  │                            peer.NewClient() ──► peer.Handshake
  │
  ├─ download_piece ────────► torrent.ParseFile()
  │                            tracker.GetPeers()
  │                            download.DownloadPiece() ──► peer.Client
  │
  ├─ download ──────────────► torrent.ParseFile()
  │                            tracker.GetPeers()
  │                            download.DownloadFile() ──► peer.Client (per piece)
  │
  ├─ magnet_parse ──────────► torrent.ParseMagnet()
  │
  ├─ magnet_handshake ──────► torrent.ParseMagnet()
  │                            tracker.GetPeers()
  │                            peer.NewClient(extensions=true)
  │                            peer.ExtensionHandshake()
  │
  ├─ magnet_info ───────────► torrent.ParseMagnet()
  │                            tracker.GetPeers()
  │                            peer.NewClient(extensions=true)
  │                            peer.FetchMetadata() ──► TorrentFile
  │
  ├─ magnet_download_piece ─► torrent.ParseMagnet()
  │                            tracker.GetPeers()
  │                            peer.FetchMetadata()
  │                            download.DownloadPiece()
  │
  └─ magnet_download ───────► torrent.ParseMagnet()
                               tracker.GetPeers()
                               peer.FetchMetadata()
                               download.DownloadFile()
```

---

## Key Types

```go
// bencode/decode.go
func Decode(input string) (interface{}, error)
func DecodeDict(input string, pos *int) (map[string]interface{}, map[string]string, error)

// torrent/torrent.go
type TorrentFile struct {
    Announce    string
    InfoHash    [20]byte
    PieceHashes [][20]byte
    PieceLength int
    Length      int
    Name        string
}

// torrent/magnet.go
type MagnetLink struct {
    InfoHash    [20]byte
    InfoHashHex string
    Name        string
    Trackers    []string
}

// peer/peer.go
type Peer struct {
    IP   net.IP
    Port uint16
}

// peer/message.go
type Message struct {
    ID      uint8
    Payload []byte
}

const (
    MsgChoke       uint8 = 0
    MsgUnchoke     uint8 = 1
    MsgInterested  uint8 = 2
    MsgBitfield    uint8 = 5
    MsgRequest     uint8 = 6
    MsgPiece       uint8 = 7
    MsgExtension   uint8 = 20
)

// peer/client.go
type Client struct {
    Conn              net.Conn
    InfoHash          [20]byte
    PeerID            [20]byte
    ExtensionsEnabled bool
}
```

---

## Extension Protocol (Implemented)

The client supports BEP 10 (Extension Protocol) and BEP 9 (Metadata Exchange):

| Feature                                            | Status |
| -------------------------------------------------- | ------ |
| Extension bit (bit 20) in handshake reserved bytes | ✅     |
| Extension handshake (msg ID 20, ext ID 0)          | ✅     |
| `ut_metadata` negotiation                          | ✅     |
| Metadata request (`msg_type: 0`)                   | ✅     |
| Metadata data response parsing (`msg_type: 1`)     | ✅     |
| Info hash verification of received metadata        | ✅     |

---

## Magnet Link vs .torrent File

| Concern      | `.torrent` file           | Magnet link                                       |
| ------------ | ------------------------- | ------------------------------------------------- |
| Info hash    | From bencoded `info` dict | From magnet URI (`xt=urn:btih:...`)               |
| Piece hashes | In torrent file           | Fetched from peers via extension protocol (BEP 9) |
| Tracker URL  | `announce` field          | `tr=` parameter in magnet URI                     |
| Metadata     | Available upfront         | Downloaded via `ut_metadata` extension messages   |

Once metadata is obtained from peers, it produces the same info dictionary, so the download logic works identically for both paths.

---

## Planned Features

- [ ] **Multi-file torrents** — Support `files` list in the info dictionary instead of single `length`
- [ ] **UDP trackers** — BEP 15 compact protocol for tracker communication
- [ ] **DHT (peer discovery)** — BEP 5 distributed hash table for trackerless peer finding
- [ ] **Refactor into packages** — Extract logic from monolithic `main.go` into the proposed package layout
- [ ] **Concurrent piece downloads** — Download from multiple peers simultaneously
