package bittorrent

import "fmt"

// Bitfield represents which pieces a peer has.
type Bitfield []byte

// HasPiece reports whether the bit for a piece index is set.
func (bf Bitfield) HasPiece(index int) bool {
	byteIndex := index / 8
	offset := index % 8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return false
	}
	return bf[byteIndex]>>(7-offset)&1 != 0
}

// SetPiece marks a piece as present in the bitfield.
func (bf Bitfield) SetPiece(index int) {
	byteIndex := index / 8
	offset := index % 8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return
	}
	bf[byteIndex] |= 1 << (7 - offset)
}

func (bf Bitfield) hasIndex(index int) error {
	byteIndex := index / 8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return fmt.Errorf("piece index %d out of range", index)
	}
	return nil
}
