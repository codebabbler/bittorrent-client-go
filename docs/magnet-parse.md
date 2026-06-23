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
| `xt`      | **Yes**  | `urn:btih:` followed by the 40-character hex-encoded info hash |
| `dn`      | No       | Display name — shown to the user while waiting for metadata    |
| `tr`      | No       | Tracker URL (may appear multiple times for multiple trackers)  |
| `x.pe`    | No       | Peer address as `hostname:port`, for direct metadata transfer  |

### Example

```
magnet:?xt=urn:btih:ad42ce8109f54c99613ce38f9b4d87e70f24a165&dn=magnet1.gif&tr=http%3A%2F%2Fbittorrent-test-tracker.codecrafters.io%2Fannounce
```

Breaking it down:

- **Info Hash:** `ad42ce8109f54c99613ce38f9b4d87e70f24a165`
- **Display Name:** `magnet1.gif`
- **Tracker URL:** `http://bittorrent-test-tracker.codecrafters.io/announce` (URL-decoded from `http%3A%2F%2F...`)

> **Note:** The `tr` value is URL-encoded in the magnet link. You must decode it to get the actual tracker URL.

---

## Step-by-Step Implementation (Go)

### 1. Parse the Magnet Link as a URL

Go's `net/url` package can parse the magnet URI and extract its query parameters:

```go
magnetLink := os.Args[2]

parsedUrl, err := url.Parse(magnetLink)
if err != nil {
    fmt.Fprintln(os.Stderr, "Error parsing magnet link:", err)
    os.Exit(1)
}
```

### 2. Extract the Info Hash

The `xt` (exact topic) parameter contains `urn:btih:` followed by the 40-char hex info hash. Strip the prefix to get just the hash:

```go
xt := parsedUrl.Query().Get("xt")
if xt == "" {
    fmt.Fprintln(os.Stderr, "Error: missing xt parameter")
    os.Exit(1)
}

// Strip the "urn:btih:" prefix
infoHash := xt[len("urn:btih:"):]
```

The resulting `infoHash` is a 40-character hex string like `ad42ce8109f54c99613ce38f9b4d87e70f24a165`.

### 3. Extract the Tracker URL

The `tr` query parameter holds the tracker URL. Go's `url.Parse` automatically handles URL-decoding of query parameter values:

```go
trackerUrl := parsedUrl.Query().Get("tr")
```

> `url.Query().Get()` returns the **decoded** value, so `http%3A%2F%2F...` becomes `http://...` automatically.

### 4. Print the Results

```go
fmt.Println("Tracker URL:", trackerUrl)
fmt.Println("Info Hash:", infoHash)
```

---

## Complete `magnet_parse` Command Example

```go
case "magnet_parse":
    if len(os.Args) < 3 {
        fmt.Fprintln(os.Stderr, "Usage: ./runner.sh magnet_parse <magnet-link>")
        os.Exit(1)
    }

    magnetLink := os.Args[2]

    parsedUrl, err := url.Parse(magnetLink)
    if err != nil {
        fmt.Fprintln(os.Stderr, "Error parsing magnet link:", err)
        os.Exit(1)
    }

    // Extract info hash from xt parameter (strip "urn:btih:" prefix)
    xt := parsedUrl.Query().Get("xt")
    if xt == "" {
        fmt.Fprintln(os.Stderr, "Error: missing xt parameter")
        os.Exit(1)
    }
    infoHash := xt[len("urn:btih:"):]

    // Extract tracker URL (automatically URL-decoded)
    trackerUrl := parsedUrl.Query().Get("tr")

    fmt.Println("Tracker URL:", trackerUrl)
    fmt.Println("Info Hash:", infoHash)
```

> **No new imports needed** — `"net/url"` is already imported in the project.

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

1. **Advertise support** — Include `ut_metadata` in the extension handshake's `m` dictionary, along with `metadata_size`.
2. **Request pieces** — The metadata is broken into 16 KiB blocks. Send `request` messages (`msg_type: 0`) for each piece.
3. **Receive data** — Peers respond with `data` messages (`msg_type: 1`) containing the metadata block appended after the bencoded header.
4. **Verify** — Once all pieces are assembled, SHA-1 hash the result and verify it matches the info hash from the magnet link.
5. **Reject** — Peers that don't have the full metadata respond with `reject` (`msg_type: 2`).

This will be implemented in later stages.
