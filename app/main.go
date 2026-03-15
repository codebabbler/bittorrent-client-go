package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

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

		peers, err := tracker.GetPeers(tf.Announce, tf.InfoHash[:], tf.Length)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		// Concurrent download from multiple peers (up to 5 workers)
		fileData, err := download.ConcurrentFile(peers, tf.InfoHash[:], tf.Pieces, tf.Length, tf.PieceLength, 5)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		if tf.IsMultiFile {
			err = download.WriteFiles(outputPath, tf, fileData)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error writing files:", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Downloaded multi-file torrent to %s/%s/\n", outputPath, tf.Name)
		} else {
			err = os.WriteFile(outputPath, fileData, 0644)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error writing file:", err)
				os.Exit(1)
			}
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

		peers, err := tracker.GetPeers(magnet.TrackerURL, magnet.InfoHash, 999)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client, err := peer.NewClient(peers[0].Address(), magnet.InfoHash, true)
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

		peers, err := tracker.GetPeers(magnet.TrackerURL, magnet.InfoHash, 999)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client, err := peer.NewClient(peers[0].Address(), magnet.InfoHash, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer client.Close()

		err = client.SetupExtensions()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

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

		peers, err := tracker.GetPeers(magnet.TrackerURL, magnet.InfoHash, 999)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client, err := peer.NewClient(peers[0].Address(), magnet.InfoHash, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer client.Close()

		err = client.SetupExtensions()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

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

		peers, err := tracker.GetPeers(magnet.TrackerURL, magnet.InfoHash, 999)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		client, err := peer.NewClient(peers[0].Address(), magnet.InfoHash, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer client.Close()

		err = client.SetupExtensions()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

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

		fileData, err := download.File(client, tf.Pieces, tf.Length, tf.PieceLength)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		if tf.IsMultiFile {
			err = download.WriteFiles(outputPath, tf, fileData)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error writing files:", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Downloaded multi-file torrent to %s/%s/\n", outputPath, tf.Name)
		} else {
			err = os.WriteFile(outputPath, fileData, 0644)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error writing file:", err)
				os.Exit(1)
			}
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
