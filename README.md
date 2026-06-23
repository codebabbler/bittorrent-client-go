# BitTorrent Client in Go

A full-featured BitTorrent client written from scratch in Go. This client implements core BitTorrent protocols including bencode decoding, peer wire protocol, HTTP/UDP tracker communications, magnet links with metadata exchange extensions, Kademlia DHT peer discovery, concurrent downloading, and seeding.

---

## Features

- **Bencode Decoder**: Custom bencode parser that supports decoding of integers, strings, lists, and dictionaries.
- **Torrent File Parsing**: Decodes `.torrent` files and computes the Info Hash (SHA-1) required for tracking and peer connections.
- **HTTP & UDP Tracker Support**: Communicates with trackers via both HTTP/HTTPS and UDP (`udp://` protocol via BEP 15) to discover active peers.
- **Peer Wire Protocol**: Full TCP state machine implementation handling handshakes, bitfield transmission, choke/unchoke notifications, interested indicators, and block requests.
- **Extension Protocol (BEP 10 & BEP 9)**: Supports extension handshakes and metadata exchange to retrieve `.torrent` metadata directly from peers using magnet links.
- **Concurrent Piece Downloader**: Downloads pieces in parallel across multiple peer connections with worker pools, verifies integrity via piece SHA-1 hashes, and handles both single-file and multi-file torrent layouts.
- **Kademlia DHT Peer Discovery (BEP 5)**: Queries the Distributed Hash Table (DHT) to resolve info hashes and find active peers without relying on a tracker.
- **Seeding Server**: Acts as an active peer by listening on TCP ports, establishing incoming handshakes, advertising available pieces via bitfields, and seeding requested blocks back to the network.

---

## Directory Structure

The project has been refactored into modular Go packages:

```text
bittorrent-client-go/
├── app/
│   └── main.go              # CLI entry point - handles args and dispatches commands
├── bencode/
│   └── decode.go            # Bencode decoding implementation
├── dht/
│   ├── dht.go               # DHT node, bootstrapping, and peer lookup
│   ├── krpc.go              # KRPC protocol encoding and decoding
│   ├── node.go              # Node and routing table definitions
│   └── routing.go           # Routing table management and XOR distance calculations
├── download/
│   ├── concurrent.go        # Concurrent downloader with worker pool
│   ├── download.go          # Sequential piece downloading and verification
│   └── filewriter.go        # Reconstructing flat buffers into single/multi-file layouts
├── peer/
│   ├── client.go            # Peer client connection, bitfields, choking state
│   ├── extension.go         # BEP 10/9 extension message and metadata handlers
│   ├── handshake.go         # TCP handshake serialization and parsing
│   └── message.go           # Peer wire message protocol parsing
├── seeder/
│   ├── handler.go           # Seeding handler for incoming peer connections
│   ├── server.go            # Seeding TCP listener and coordinator
│   └── storage.go           # Pieces memory loading and verification
├── torrent/
│   ├── magnet.go            # Magnet link parsing and metadata mapping
│   └── torrent.go           # Torrent file structure and parsing logic
├── tracker/
│   ├── tracker.go           # HTTP/HTTPS tracker client
│   └── udp.go               # UDP tracker client (BEP 15)
├── docs/                    # Deep-dive protocol and architectural documentation
├── runner.sh                # Helper script to build and run the binary
└── sample.torrent           # Sample torrent file for testing
```

For detailed specifications, see the documentation in the [docs](./docs/) directory.

---

## Build & Installation

### Prerequisites
- [Go 1.25+](https://go.dev/)

### Building the Project
Use the provided `runner.sh` script, which compiles the application and runs the built binary:
```bash
chmod +x runner.sh
./runner.sh <command> [args]
```
Alternatively, you can compile and run directly using standard Go tooling:
```bash
go build -o bittorrent-go app/*.go
./bittorrent-go <command> [args]
```

---

## CLI Usage & Commands

### 1. Decode Bencode
Decodes a bencoded string and outputs it in JSON format.
```bash
./runner.sh decode <bencoded_value>
```
*Example:*
```bash
./runner.sh decode d3:bar4:spam4:gool3:onei2ee3:foo3:bare
# Output: {"bar":"spam","foo":"bar","goo":[1,2]}
```

### 2. Torrent File Information
Parses a `.torrent` file and outputs details like Tracker URL, Info Hash, Length, Piece Length, and individual piece hashes.
```bash
./runner.sh info <torrent_file>
```
*Example:*
```bash
./runner.sh info sample.torrent
```

### 3. Discover Peers (Tracker)
Discovers peers by contacting the torrent's tracker.
```bash
./runner.sh peers <torrent_file>
```
*Example:*
```bash
./runner.sh peers sample.torrent
```

### 4. Peer Handshake
Performs a TCP handshake with a specific peer.
```bash
./runner.sh handshake <torrent_file> <peer_address>
```
*Example:*
```bash
./runner.sh handshake sample.torrent 127.0.0.1:6881
```

### 5. Download a Single Piece
Downloads a single piece of the torrent to a target path.
```bash
./runner.sh download_piece -o <output_file> <torrent_file> <piece_index>
```
*Example:*
```bash
./runner.sh download_piece -o /tmp/piece_0 sample.torrent 0
```

### 6. Download Entire File
Downloads the entire file from peers using a concurrent worker pool.
```bash
./runner.sh download -o <output_path> <torrent_file>
```
*Example:*
```bash
./runner.sh download -o ./downloaded_file sample.torrent
```

### 7. Parse Magnet Link
Parses a magnet link and extracts the Tracker URL and Info Hash.
```bash
./runner.sh magnet_parse <magnet-link>
```
*Example:*
```bash
./runner.sh magnet_parse "magnet:?xt=urn:btih:ad32b137d6e6c1e550e50f421e42b26090e54b6d&dn=sample&tr=http%3A%2F%2Ftracker.co%3A80%2Fannounce"
```

### 8. Magnet Handshake
Performs a peer handshake for a magnet link, validating extension protocol support (BEP 10).
```bash
./runner.sh magnet_handshake <magnet-link>
```

### 9. Magnet Info
Retrieves torrent metadata directly from peers using the BEP 9 extension protocol over a magnet link.
```bash
./runner.sh magnet_info <magnet-link>
```

### 10. Magnet Piece Download
Downloads a specific piece using a magnet link.
```bash
./runner.sh magnet_download_piece -o <output_path> <magnet-link> <piece_index>
```

### 11. Magnet Full Download
Downloads the entire torrent contents from scratch using only a magnet link.
```bash
./runner.sh magnet_download -o <output_path> <magnet-link>
```

### 12. DHT Peer Discovery
Discovers peers for a given info hash using the Kademlia DHT routing system (BEP 5).
```bash
./runner.sh dht_peers <info_hash_hex>
```
*Example:*
```bash
./runner.sh dht_peers ad32b137d6e6c1e550e50f421e42b26090e54b6d
```

### 13. Seed a Torrent
Acts as a seeder, accepting incoming TCP connections and serving pieces to the network.
```bash
./runner.sh seed <torrent_file> <data_path>
```
*Example:*
```bash
./runner.sh seed sample.torrent ./downloaded_file
```

---

## Deep-Dive Specifications
To understand the underlying protocol implementations, refer to the documentation files under the [`docs/`](./docs/) directory:
- [Project Architecture & Directory Layout](./docs/project-structure.md)
- [Peer Handshake Mechanism](./docs/peer-handshake.md)
- [Sequential Piece Downloader](./docs/download-piece.md)
- [Concurrent File Downloader](./docs/download-file.md)
- [Magnet Link Specification](./docs/magnet-parse.md)
- [Extension Handshake Negotiation](./docs/extension-handshake.md)
- [Metadata Extension Exchange (BEP 9)](./docs/magnet-info.md)
- [DHT & UDP Trackers Design](./docs/advanced-features-plan.md)
