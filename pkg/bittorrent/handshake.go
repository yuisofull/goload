package bittorrent

import (
	"fmt"
	"io"
)

const protocolIdentifier = "BitTorrent protocol"

// Handshake is the initial peer protocol handshake.
type Handshake struct {
	Pstr     string
	InfoHash [20]byte
	PeerID   [20]byte
}

// NewHandshake builds a protocol handshake payload.
func NewHandshake(infoHash, peerID [20]byte) *Handshake {
	return &Handshake{Pstr: protocolIdentifier, InfoHash: infoHash, PeerID: peerID}
}

// Serialize serializes the handshake.
func (h *Handshake) Serialize() []byte {
	buf := make([]byte, len(h.Pstr)+49)
	buf[0] = byte(len(h.Pstr))
	curr := 1
	curr += copy(buf[curr:], h.Pstr)
	curr += copy(buf[curr:], make([]byte, 8))
	curr += copy(buf[curr:], h.InfoHash[:])
	curr += copy(buf[curr:], h.PeerID[:])
	return buf
}

// ReadHandshake parses a handshake from a stream.
func ReadHandshake(r io.Reader) (*Handshake, error) {
	lengthBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, lengthBuf); err != nil {
		return nil, err
	}
	pstrlen := int(lengthBuf[0])
	if pstrlen == 0 {
		return nil, fmt.Errorf("invalid handshake protocol length")
	}

	handshakeBuf := make([]byte, 48+pstrlen)
	if _, err := io.ReadFull(r, handshakeBuf); err != nil {
		return nil, err
	}

	var infoHash, peerID [20]byte
	copy(infoHash[:], handshakeBuf[pstrlen+8:pstrlen+8+20])
	copy(peerID[:], handshakeBuf[pstrlen+8+20:])

	return &Handshake{
		Pstr:     string(handshakeBuf[:pstrlen]),
		InfoHash: infoHash,
		PeerID:   peerID,
	}, nil
}
