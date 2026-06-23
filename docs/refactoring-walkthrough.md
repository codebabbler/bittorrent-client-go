# Refactoring Walkthrough: main.go → Packages

Refactored `app/main.go` from **~1980 lines** to **~330 lines** by extracting logic into 6 packages.

## Package Structure

| Package     | Files                                                     | Purpose                                                                                                                               |
| ----------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `bencode/`  | `decode.go`                                               | Bencode decoding: `Decode`, `DecodeDict`, `DecodeString`, `DecodeInteger`, `DecodeList`                                               |
| `torrent/`  | `torrent.go`, `magnet.go`                                 | `TorrentFile` struct + `ParseFile`; `MagnetLink` struct + `ParseMagnet` + `ToTorrentFile`                                             |
| `tracker/`  | `tracker.go`                                              | `Peer` struct + `GetPeers` (HTTP tracker)                                                                                             |
| `peer/`     | `handshake.go`, `message.go`, `extension.go`, `client.go` | `Client` struct: `NewClient`, `DoHandshake`, `ReadMessage`/`WriteMessage`, `DoExtensionHandshake`, `RequestMetadata`, `DownloadPiece` |
| `download/` | `download.go`                                             | `Piece` (single piece + hash verify), `File` (all pieces)                                                                             |

## Dependency Graph

```
app/main.go
├── bencode
├── torrent   → bencode
├── tracker   → bencode
├── peer      → bencode
└── download  → peer
```

## Command Mapping

Each `case` in `app/main.go` composes package calls:

| Command                 | Packages Used                                                                            |
| ----------------------- | ---------------------------------------------------------------------------------------- |
| `decode`                | `bencode.Decode`                                                                         |
| `info`                  | `torrent.ParseFile`                                                                      |
| `peers`                 | `torrent.ParseFile` → `tracker.GetPeers`                                                 |
| `handshake`             | `torrent.ParseFile` → `peer.NewClient`                                                   |
| `download_piece`        | `torrent.ParseFile` → `tracker.GetPeers` → `peer.NewClient` → `download.Piece`           |
| `download`              | `torrent.ParseFile` → `tracker.GetPeers` → `peer.NewClient` → `download.File`            |
| `magnet_parse`          | `torrent.ParseMagnet`                                                                    |
| `magnet_handshake`      | `torrent.ParseMagnet` → `tracker.GetPeers` → `peer.NewClient` → `client.SetupExtensions` |
| `magnet_info`           | above + `client.FetchMetadata` → `magnet.ToTorrentFile`                                  |
| `magnet_download_piece` | above + `download.Piece`                                                                 |
| `magnet_download`       | above + `download.File`                                                                  |

## Verification

```bash
go build ./...                          # ✅ Compiles cleanly
go run ./app decode '5:hello'           # → "hello"
go run ./app decode 'li52ei42e3:fooe'   # → [52,42,"foo"]
go run ./app info sample.torrent        # → Tracker URL, Length, Info Hash, Piece Hashes
```
