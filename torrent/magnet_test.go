package torrent

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestParseMagnet(t *testing.T) {
	// 40-char Hex info hash
	hexMagnet := "magnet:?xt=urn:btih:49046DBCC44100FCF062551B0CF467D8AA513451&dn=Judge+Stone&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce"
	m1, err := ParseMagnet(hexMagnet)
	if err != nil {
		t.Fatalf("Failed to parse hex magnet: %v", err)
	}

	expectedHash, _ := hex.DecodeString("49046dbcc44100fcf062551b0cf467d8aa513451")
	if !bytes.Equal(m1.InfoHash, expectedHash) {
		t.Errorf("Hex hash mismatch: got %x, expected %x", m1.InfoHash, expectedHash)
	}
	if m1.Name != "Judge Stone" {
		t.Errorf("Name mismatch: got %q, expected %q", m1.Name, "Judge Stone")
	}
	if m1.TrackerURL != "udp://tracker.opentrackr.org:1337/announce" {
		t.Errorf("Tracker mismatch: got %q", m1.TrackerURL)
	}

	// 32-char Base32 info hash (same hash: 49046dbcc44100fcf062551b0cf467d8aa513451 -> Base32: jecg3pgeieapz4dckunqz5dh3cvfcncr)
	base32Magnet := "magnet:?xt=urn:btih:jecg3pgeieapz4dckunqz5dh3cvfcncr&dn=Judge+Stone"
	m2, err := ParseMagnet(base32Magnet)
	if err != nil {
		t.Fatalf("Failed to parse base32 magnet: %v", err)
	}
	if !bytes.Equal(m2.InfoHash, expectedHash) {
		t.Errorf("Base32 hash mismatch: got %x, expected %x", m2.InfoHash, expectedHash)
	}

	// Invalid cases
	_, err = ParseMagnet("magnet:?xt=urn:btmh:jede3pf4ieap54dckunqz5dh3ksve5cr")
	if err == nil {
		t.Error("Expected error for non-btih prefix")
	}

	_, err = ParseMagnet("magnet:?xt=urn:btih:jede3pf4ieap54dckunqz5dh3ksve5")
	if err == nil {
		t.Error("Expected error for invalid length")
	}
}
