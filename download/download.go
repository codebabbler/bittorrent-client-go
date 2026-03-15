package download

import (
	"crypto/sha1"
	"fmt"
	"os"

	"github.com/codebabbler/bittorrent-client-go/peer"
)

// Piece downloads a single piece, verifies its hash, and returns the data.
func Piece(client *peer.Client, pieces string, pieceIndex, pieceLength int) ([]byte, error) {
	pieceData, err := client.DownloadPiece(pieceIndex, pieceLength)
	if err != nil {
		return nil, err
	}

	// Verify piece hash
	expectedHash := pieces[pieceIndex*20 : (pieceIndex+1)*20]
	actualHash := sha1.Sum(pieceData)
	if string(actualHash[:]) != expectedHash {
		return nil, fmt.Errorf("hash mismatch for piece %d", pieceIndex)
	}

	return pieceData, nil
}

// File downloads all pieces, verifies each, concatenates, and returns the complete file.
func File(client *peer.Client, pieces string, totalLength, normalPieceLength int) ([]byte, error) {
	totalPieces := (totalLength + normalPieceLength - 1) / normalPieceLength
	fileData := make([]byte, 0, totalLength)

	for i := 0; i < totalPieces; i++ {
		pieceLength := normalPieceLength
		if i == totalPieces-1 {
			pieceLength = totalLength - (i * normalPieceLength)
		}

		pieceData, err := Piece(client, pieces, i, pieceLength)
		if err != nil {
			return nil, err
		}

		fileData = append(fileData, pieceData...)
		fmt.Fprintf(os.Stderr, "Piece %d/%d downloaded and verified.\n", i+1, totalPieces)
	}

	return fileData, nil
}
