package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/codebabbler/bittorrent-client-go/bencode"
	"github.com/codebabbler/bittorrent-client-go/dht"
	"github.com/codebabbler/bittorrent-client-go/download"
	"github.com/codebabbler/bittorrent-client-go/peer"
	"github.com/codebabbler/bittorrent-client-go/seeder"
	"github.com/codebabbler/bittorrent-client-go/torrent"
	"github.com/codebabbler/bittorrent-client-go/tracker"
)

func main() {
	fmt.Fprintln(os.Stderr, "Logs from your program will appear here!")

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: ./runner.sh <command> [args]")
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "decode":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh decode <bencoded_value>")
			os.Exit(1)
		}

		decoded, err := bencode.Decode(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		jsonOutput, err := json.Marshal(decoded)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error marshalling JSON:", err)
			os.Exit(1)
		}

		fmt.Println(string(jsonOutput))

	case "info":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh info <torrent_file>")
			os.Exit(1)
		}

		tf, err := torrent.ParseFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		fmt.Println("Tracker URL:", tf.Announce)
		fmt.Println("Length:", tf.Length)
		fmt.Println("Piece Length:", tf.PieceLength)
		fmt.Println("Info Hash:", tf.InfoHashHex)
		fmt.Println("Piece Hashes:")
		for _, h := range tf.PieceHashes() {
			fmt.Println(h)
		}

	case "peers":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh peers <torrent_file>")
			os.Exit(1)
		}

		tf, err := torrent.ParseFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		peers, err := tracker.GetPeers(tf.Announce, tf.InfoHash[:], tf.Length)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		for _, p := range peers {
			fmt.Printf("%s:%d\n", p.IP, p.Port)
		}

	case "handshake":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh handshake <torrent_file> <peer_address>")
			os.Exit(1)
		}

		tf, err := torrent.ParseFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client, err := peer.NewClient(os.Args[3], tf.InfoHash[:], false)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer client.Close()

		fmt.Println("Peer ID:", hex.EncodeToString(client.PeerID[:]))

	case "download_piece":
		if len(os.Args) < 6 || os.Args[2] != "-o" {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh download_piece -o <output_file> <torrent_file> <piece_index>")
			os.Exit(1)
		}

		outputPath := os.Args[3]
		torrentFile := os.Args[4]
		pieceIndex, err := strconv.Atoi(os.Args[5])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: invalid piece index")
			os.Exit(1)
		}

		tf, err := torrent.ParseFile(torrentFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		peers, err := tracker.GetPeers(tf.Announce, tf.InfoHash[:], tf.Length)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client, err := peer.NewClient(peers[0].Address(), tf.InfoHash[:], false)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer client.Close()

		err = client.SendInterested()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		err = client.WaitForUnchoke()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		// Calculate actual piece length
		pieceLength := tf.PieceLength
		totalPieces := (tf.Length + tf.PieceLength - 1) / tf.PieceLength
		if pieceIndex == totalPieces-1 {
			pieceLength = tf.Length - (pieceIndex * tf.PieceLength)
		}

		data, err := download.Piece(client, tf.Pieces, pieceIndex, pieceLength)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		err = os.WriteFile(outputPath, data, 0644)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error writing file:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Piece %d downloaded to %s.\n", pieceIndex, outputPath)

	case "download":
		if len(os.Args) < 5 || os.Args[2] != "-o" {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh download -o <output_file> <torrent_file>")
			os.Exit(1)
		}

		outputPath := os.Args[3]
		torrentFile := os.Args[4]

		tf, err := torrent.ParseFile(torrentFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		targetOutput := outputPath
		if tf.IsMultiFile {
			targetOutput = outputPath + ".tmp"
		} else {
			info, err := os.Stat(outputPath)
			if err == nil && info.IsDir() {
				outputPath = filepath.Join(outputPath, tf.Name)
				targetOutput = outputPath
			}
		}

		sess := download.NewSession(30, 15)
		discoverPeers := func() ([]tracker.Peer, error) {
			return tracker.GetPeers(tf.Announce, tf.InfoHash[:], tf.Length)
		}

		err = sess.Download(tf.InfoHash[:], tf.Pieces, tf.Length, tf.PieceLength, targetOutput, discoverPeers)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		if tf.IsMultiFile {
			fileData, err := os.ReadFile(targetOutput)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error reading temp file:", err)
				os.Exit(1)
			}
			os.Remove(targetOutput)

			err = download.WriteFiles(outputPath, tf, fileData)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error writing files:", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Downloaded multi-file torrent to %s/%s/\n", outputPath, tf.Name)
		} else {
			fmt.Fprintf(os.Stderr, "Downloaded %s to %s.\n", torrentFile, outputPath)
		}

	case "magnet_parse":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh magnet_parse <magnet-link>")
			os.Exit(1)
		}

		magnet, err := torrent.ParseMagnet(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		fmt.Println("Tracker URL:", magnet.TrackerURL)
		fmt.Println("Info Hash:", magnet.InfoHashHex)

	case "magnet_handshake":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./your_program.sh magnet_handshake <magnet-link>")
			os.Exit(1)
		}

		magnet, err := torrent.ParseMagnet(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		peers, err := discoverPeersForMagnet(magnet)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client, err := connectToWorkingPeer(peers, magnet.InfoHash, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer client.Close()

		fmt.Println("Peer ID:", hex.EncodeToString(client.PeerID[:]))

		err = client.SetupExtensions()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		fmt.Println("Peer Metadata Extension ID:", client.PeerMetadataExtId)

	case "magnet_info":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh magnet_info <magnet-link>")
			os.Exit(1)
		}

		magnet, err := torrent.ParseMagnet(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		peers, err := discoverPeersForMagnet(magnet)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client, err := connectToWorkingPeer(peers, magnet.InfoHash, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer client.Close()

		rawMetadata, err := client.FetchMetadata()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		tf, err := magnet.ToTorrentFile(rawMetadata)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		fmt.Println("Tracker URL:", magnet.TrackerURL)
		fmt.Println("Length:", tf.Length)
		fmt.Println("Info Hash:", magnet.InfoHashHex)
		fmt.Println("Piece Length:", tf.PieceLength)
		fmt.Println("Piece Hashes:")
		for _, h := range tf.PieceHashes() {
			fmt.Println(h)
		}

	case "magnet_download_piece":
		if len(os.Args) < 6 || os.Args[2] != "-o" {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh magnet_download_piece -o <output> <magnet-link> <piece_index>")
			os.Exit(1)
		}

		outputPath := os.Args[3]
		magnetLink := os.Args[4]
		pieceIndex, err := strconv.Atoi(os.Args[5])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: invalid piece index")
			os.Exit(1)
		}

		magnet, err := torrent.ParseMagnet(magnetLink)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		peers, err := discoverPeersForMagnet(magnet)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client, err := connectToWorkingPeer(peers, magnet.InfoHash, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer client.Close()

		rawMetadata, err := client.FetchMetadata()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		tf, err := magnet.ToTorrentFile(rawMetadata)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		err = client.SendInterested()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		err = client.WaitForUnchoke()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		// Calculate actual piece length
		pieceLength := tf.PieceLength
		totalPieces := (tf.Length + tf.PieceLength - 1) / tf.PieceLength
		if pieceIndex == totalPieces-1 {
			pieceLength = tf.Length - (pieceIndex * tf.PieceLength)
		}

		data, err := download.Piece(client, tf.Pieces, pieceIndex, pieceLength)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		err = os.WriteFile(outputPath, data, 0644)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error writing file:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Piece %d downloaded to %s.\n", pieceIndex, outputPath)

	case "magnet_download":
		if len(os.Args) < 5 || os.Args[2] != "-o" {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh magnet_download -o <output> <magnet-link>")
			os.Exit(1)
		}

		outputPath := os.Args[3]
		magnetLink := os.Args[4]

		magnet, err := torrent.ParseMagnet(magnetLink)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		peers, err := discoverPeersForMagnet(magnet)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client, err := connectToWorkingPeer(peers, magnet.InfoHash, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer client.Close()

		rawMetadata, err := client.FetchMetadata()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		tf, err := magnet.ToTorrentFile(rawMetadata)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client.Close()

		targetOutput := outputPath
		if tf.IsMultiFile {
			targetOutput = outputPath + ".tmp"
		} else {
			info, err := os.Stat(outputPath)
			if err == nil && info.IsDir() {
				outputPath = filepath.Join(outputPath, tf.Name)
				targetOutput = outputPath
			}
		}

		sess := download.NewSession(30, 15)
		discoverPeers := func() ([]tracker.Peer, error) {
			return discoverPeersForMagnet(magnet)
		}

		err = sess.Download(tf.InfoHash[:], tf.Pieces, tf.Length, tf.PieceLength, targetOutput, discoverPeers)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		if tf.IsMultiFile {
			fileData, err := os.ReadFile(targetOutput)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error reading temp file:", err)
				os.Exit(1)
			}
			os.Remove(targetOutput)

			err = download.WriteFiles(outputPath, tf, fileData)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error writing files:", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Downloaded multi-file torrent to %s/%s/\n", outputPath, tf.Name)
		} else {
			fmt.Fprintf(os.Stderr, "Downloaded to %s.\n", outputPath)
		}

	case "dht_peers":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh dht_peers <info_hash_hex>")
			os.Exit(1)
		}

		infoHashHex := os.Args[2]
		infoHashBytes, err := hex.DecodeString(infoHashHex)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error decoding info hash:", err)
			os.Exit(1)
		}

		var infoHash [20]byte
		copy(infoHash[:], infoHashBytes)

		d, err := dht.New(6881)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error creating DHT:", err)
			os.Exit(1)
		}
		defer d.Close()

		err = d.Bootstrap(dht.DefaultBootstrapNodes)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error bootstrapping DHT:", err)
			os.Exit(1)
		}

		peers, err := d.GetPeers(infoHash)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error finding peers:", err)
			os.Exit(1)
		}

		for _, p := range peers {
			fmt.Printf("%s:%d\n", p.IP, p.Port)
		}

	case "seed":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh seed <torrent_file> <data_path>")
			os.Exit(1)
		}

		torrentFile := os.Args[2]
		dataPath := os.Args[3]

		tf, err := torrent.ParseFile(torrentFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		srv, err := seeder.New(6882, tf, dataPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer srv.Stop()

		fmt.Printf("Seeding on port %d. Press Ctrl+C to stop.\n", srv.Port())
		srv.Start() // blocks

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		os.Exit(1)
	}
}

func discoverPeersForMagnet(magnet *torrent.MagnetLink) ([]tracker.Peer, error) {
	var allPeers []tracker.Peer
	seenPeers := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	addPeers := func(peers []tracker.Peer, source string) {
		mu.Lock()
		defer mu.Unlock()
		added := 0
		for _, p := range peers {
			addr := p.Address()
			if !seenPeers[addr] {
				seenPeers[addr] = true
				allPeers = append(allPeers, p)
				added++
			}
		}
		if added > 0 {
			fmt.Fprintf(os.Stderr, "Discovered %d new peers from %s\n", added, source)
		}
	}

	// 1. Gather all tracker URLs to query
	var trackerURLs []string
	if len(magnet.TrackerURLs) > 0 {
		trackerURLs = magnet.TrackerURLs
	} else if magnet.TrackerURL != "" {
		trackerURLs = []string{magnet.TrackerURL}
	}

	// 2. Query trackers in parallel
	if len(trackerURLs) > 0 {
		fmt.Fprintf(os.Stderr, "Querying %d trackers in parallel...\n", len(trackerURLs))
		for _, trURL := range trackerURLs {
			wg.Add(1)
			go func(urlStr string) {
				defer wg.Done()
				peers, err := tracker.GetPeers(urlStr, magnet.InfoHash, 999)
				if err != nil {
					return
				}
				addPeers(peers, urlStr)
			}(trURL)
		}
	}

	// 3. Query DHT in parallel
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Fprintln(os.Stderr, "Querying DHT for peers...")
		d, err := dht.New(0) // bind to ephemeral port
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start DHT: %v\n", err)
			return
		}
		defer d.Close()
		err = d.Bootstrap(dht.DefaultBootstrapNodes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DHT bootstrap error: %v\n", err)
			return
		}
		var infoHash [20]byte
		copy(infoHash[:], magnet.InfoHash)
		peers, err := d.GetPeers(infoHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DHT lookup error: %v\n", err)
			return
		}
		addPeers(peers, "DHT")
	}()

	// Wait for all discovery tasks to complete
	wg.Wait()

	if len(allPeers) == 0 {
		return nil, fmt.Errorf("could not find any peers via trackers or DHT")
	}

	return allPeers, nil
}

func connectToWorkingPeer(peers []tracker.Peer, infoHash []byte, setupExtensions bool) (*peer.Client, error) {
	if len(peers) == 0 {
		return nil, fmt.Errorf("no peers to connect to")
	}

	resultChan := make(chan *peer.Client, 1)
	done := make(chan struct{})

	var mu sync.Mutex
	var completed bool

	var wg sync.WaitGroup
	// Limit concurrency to 30 parallel dialing goroutines
	sem := make(chan struct{}, 30)

	for _, p := range peers {
		wg.Add(1)
		go func(peerAddr string) {
			defer wg.Done()

			select {
			case <-done:
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			select {
			case <-done:
				return
			default:
			}

			fmt.Fprintf(os.Stderr, "Trying to connect to peer %s...\n", peerAddr)
			client, err := peer.NewClient(peerAddr, infoHash, setupExtensions)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to connect to peer %s: %v\n", peerAddr, err)
				return
			}

			if setupExtensions {
				err = client.SetupExtensions()
				if err != nil {
					client.Close()
					fmt.Fprintf(os.Stderr, "Failed extension handshake with peer %s: %v\n", peerAddr, err)
					return
				}
			}

			mu.Lock()
			if !completed {
				completed = true
				close(done) // cancel all other dials immediately
				resultChan <- client
			} else {
				client.Close()
			}
			mu.Unlock()
		}(p.Address())
	}

	// Close resultChan when all dialers finish so we don't block forever if all fail
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	client, ok := <-resultChan
	if ok && client != nil {
		return client, nil
	}

	return nil, fmt.Errorf("failed to connect to any peer")
}
