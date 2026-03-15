package seeder

import (
	"fmt"
	"net"
	"os"

	"github.com/codebabbler/bittorrent-client-go/torrent"
)

// Server listens for incoming peer connections and serves pieces.
type Server struct {
	listener  net.Listener
	infoHash  [20]byte
	pieces    [][]byte
	numPieces int
	port      int
}

// New creates a seeder server by loading the file data and verifying piece hashes.
func New(port int, tf *torrent.TorrentFile, dataPath string) (*Server, error) {
	pieces, err := LoadPieces(dataPath, tf)
	if err != nil {
		return nil, fmt.Errorf("loading pieces: %w", err)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("listening on port %d: %w", port, err)
	}

	numPieces := (tf.Length + tf.PieceLength - 1) / tf.PieceLength

	fmt.Fprintf(os.Stderr, "Seeder: loaded %d pieces, listening on port %d\n", len(pieces), port)

	return &Server{
		listener:  listener,
		infoHash:  tf.InfoHash,
		pieces:    pieces,
		numPieces: numPieces,
		port:      port,
	}, nil
}

// Start begins accepting connections. Blocks until Stop is called.
func (s *Server) Start() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Listener closed
			return
		}
		go handlePeer(conn, s.infoHash, s.pieces, s.numPieces)
	}
}

// Stop closes the listener.
func (s *Server) Stop() {
	s.listener.Close()
}

// Port returns the port the server is listening on.
func (s *Server) Port() int {
	return s.port
}
