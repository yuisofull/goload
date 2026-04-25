package bittorrent

import (
	"encoding/binary"
	"fmt"
	"io"
)

type messageID uint8

const (
	MsgChoke messageID = iota
	MsgUnchoke
	MsgInterested
	MsgNotInterested
	MsgHave
	MsgBitfield
	MsgRequest
	MsgPiece
	MsgCancel
)

// Message stores ID and payload of a BitTorrent message.
type Message struct {
	ID      messageID
	Payload []byte
}

// Serialize serializes a message into <len><id><payload>.
// Nil is interpreted as a keep-alive message.
func (m *Message) Serialize() []byte {
	if m == nil {
		return make([]byte, 4)
	}
	length := uint32(len(m.Payload) + 1)
	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[0:4], length)
	buf[4] = byte(m.ID)
	copy(buf[5:], m.Payload)
	return buf
}

// ReadMessage parses one message from a stream.
func ReadMessage(r io.Reader) (*Message, error) {
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lengthBuf); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lengthBuf)
	if length == 0 {
		return nil, nil
	}
	msgBuf := make([]byte, length)
	if _, err := io.ReadFull(r, msgBuf); err != nil {
		return nil, err
	}
	return &Message{ID: messageID(msgBuf[0]), Payload: msgBuf[1:]}, nil
}

func BuildRequest(index, begin, length int) *Message {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	binary.BigEndian.PutUint32(payload[8:12], uint32(length))
	return &Message{ID: MsgRequest, Payload: payload}
}

func BuildInterested() *Message {
	return &Message{ID: MsgInterested}
}

func BuildUnchoke() *Message {
	return &Message{ID: MsgUnchoke}
}

func BuildHave(index int) *Message {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, uint32(index))
	return &Message{ID: MsgHave, Payload: payload}
}

func ParsePiece(index int, buf []byte, msg *Message) (int, error) {
	if msg.ID != MsgPiece {
		return 0, fmt.Errorf("expected piece message, got id %d", msg.ID)
	}
	if len(msg.Payload) < 8 {
		return 0, fmt.Errorf("piece payload too short")
	}
	parsedIndex := int(binary.BigEndian.Uint32(msg.Payload[0:4]))
	if parsedIndex != index {
		return 0, fmt.Errorf("unexpected piece index: got %d want %d", parsedIndex, index)
	}
	begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
	if begin < 0 || begin >= len(buf) {
		return 0, fmt.Errorf("piece begin offset out of range: %d", begin)
	}
	data := msg.Payload[8:]
	if begin+len(data) > len(buf) {
		return 0, fmt.Errorf("piece data overflows target buffer")
	}
	copy(buf[begin:], data)
	return len(data), nil
}

func ParseHave(msg *Message) (int, error) {
	if msg.ID != MsgHave {
		return 0, fmt.Errorf("expected have message")
	}
	if len(msg.Payload) != 4 {
		return 0, fmt.Errorf("invalid have payload size")
	}
	return int(binary.BigEndian.Uint32(msg.Payload)), nil
}
