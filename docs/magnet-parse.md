# Parsing Magnet Links

## Overview

Magnet links allow users to download files from peers **without needing a `.torrent` file**. Unlike torrent files, magnet links don't contain information like file length, piece length, or piece hashes — they only include the bare minimum needed to discover peers. The rest of the metadata is obtained from peers later via the **metadata exchange protocol** ([BEP 9](https://www.bittorrent.org/beps/bep_0009.html)).

---

## Magnet Link Format

A v1 magnet link looks like this:

```
magnet:?xt=urn:btih:<info-hash>&dn=<name>&tr=<tracker-url>&x.pe=<peer-address>
```

### Query Parameters

| Parameter | Required | Description                                                    |
| --------- | -------- | -------------------------------------------------------------- |
| `xt`      | **Yes**  | `urn:btih:` followed by either a 40-character hex-encoded info hash or a 32-character Base32-encoded info hash |
| `dn`      | No       | Display name — shown to the user while waiting for metadata    |
| `tr`      | No       | Tracker URL (may appear multiple times for multiple trackers)  |
| `x.pe`    | No       | Peer address as `hostname:port`, for direct metadata transfer  |

### Example (Hexadecimal)

```
magnet:?xt=urn:btih:ad42ce8109f54c99613ce38f9b4d87e70f24a165&dn=magnet1.gif&tr=http%3A%2F%2Fbittorrent-test-tracker.codecrafters.io%2Fannounce
```

### Example (Base32)

```
magnet:?xt=urn:btih:jede3pf4ieap54dckunqz5dh3ksve5cr&dn=Judge+Stone
```

---

## Step-by-Step Implementation (Go)

### 1. Parse the Magnet Link as a URL

Go's `net/url` package can parse the magnet URI and extract its query parameters:

```go
parsedUrl, err := url.Parse(magnetLink)
if err != nil {
    return nil, fmt.Errorf("parsing magnet link: %w", err)
}
```

### 2. Extract and Validate the Info Hash Prefix

The `xt` (exact topic) parameter contains the info hash. Ensure it exists and has the correct `urn:btih:` protocol prefix to prevent out-of-bounds panics:

```go
xt := parsedUrl.Query().Get("xt")
if xt == "" {
    return nil, fmt.Errorf("missing xt parameter")
}

if !strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
    return nil, fmt.Errorf("unsupported magnet hash format: must start with urn:btih:")
}

hashStr := xt[len("urn:btih:"):]
```

### 3. Decode the Hash (Base16 or Base32)

The hash format is detected and parsed based on its string length:

```go
var infoHash []byte
switch len(hashStr) {
case 40: // Base16 (Hex)
    infoHash, err = hex.DecodeString(hashStr)
case 32: // Base32
    infoHash, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(hashStr))
default:
    return nil, fmt.Errorf("invalid info hash length: %d (expected 32 or 40)", len(hashStr))
}
```

### 4. Extract Trackers and Display Name

```go
trackers := parsedUrl.Query()["tr"]
name := parsedUrl.Query().Get("dn")
```

---

## Complete `magnet_parse` Command Implementation

```go
case "magnet_parse":
    if len(os.Args) < 3 {
        fmt.Fprintln(os.Stderr, "Usage: ./runner.sh magnet_parse <magnet-link>")
        os.Exit(1)
    }

    magnetLink, err := torrent.ParseMagnet(os.Args[2])
    if err != nil {
        fmt.Fprintln(os.Stderr, "Error parsing magnet link:", err)
        os.Exit(1)
    }

    fmt.Println("Tracker URL:", magnetLink.TrackerURL)
    fmt.Println("Info Hash:", magnetLink.InfoHashHex)
```

---

## Running the Command

```bash
./runner.sh magnet_parse "magnet:?xt=urn:btih:ad42ce8109f54c99613ce38f9b4d87e70f24a165&dn=magnet1.gif&tr=http%3A%2F%2Fbittorrent-test-tracker.codecrafters.io%2Fannounce"
```

### Expected Output

```
Tracker URL: http://bittorrent-test-tracker.codecrafters.io/announce
Info Hash: ad42ce8109f54c99613ce38f9b4d87e70f24a165
```

---

## Background: Metadata Exchange (BEP 9)

After parsing the magnet link, a client uses the info hash and tracker URL to discover peers. It then requests the **info dictionary** (the torrent metadata) from those peers using the **extension protocol** ([BEP 10](https://www.bittorrent.org/beps/bep_0010.html)):

1. **Advertise support** — Include `ut_metadata` in the extension handshake's `m` dictionary.
2. **Request pieces** — The metadata is broken into 16 KiB blocks. Send `request` messages (`msg_type: 0`) for each piece.
3. **Receive data** — Peers respond with `data` messages (`msg_type: 1`) containing the metadata block appended after the bencoded header.
4. **Verify** — Once all pieces are assembled, SHA-1 hash the result and verify it matches the info hash from the magnet link.
5. **Reject** — Peers that don't have the full metadata respond with `reject` (`msg_type: 2`).
