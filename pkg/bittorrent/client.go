package bittorrent

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"
)

// PeerClient handles protocol communication with one peer.
type PeerClient struct {
	Conn     net.Conn
	Choked   bool
	Bitfield Bitfield

	peer     Peer
	peerID   [20]byte
	infoHash [20]byte
}

// NewPeerClient dials and handshakes with a peer.
func NewPeerClient(ctx context.Context, peer Peer, peerID, infoHash [20]byte, timeout time.Duration) (*PeerClient, error) {
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", peer.String())
	if err != nil {
		return nil, err
	}

	pc := &PeerClient{
		Conn:     conn,
		Choked:   true,
		Bitfield: nil,
		peer:     peer,
		peerID:   peerID,
		infoHash: infoHash,
	}

	if err := pc.completeHandshake(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := pc.receiveBitfield(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return pc, nil
}

func (c *PeerClient) completeHandshake() error {
	if err := c.Conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	defer c.Conn.SetDeadline(time.Time{})

	req := NewHandshake(c.infoHash, c.peerID)
	if _, err := c.Conn.Write(req.Serialize()); err != nil {
		return err
	}
	resp, err := ReadHandshake(c.Conn)
	if err != nil {
		return err
	}
	if resp.Pstr != protocolIdentifier {
		return fmt.Errorf("peer uses unsupported protocol %q", resp.Pstr)
	}
	if resp.InfoHash != c.infoHash {
		return fmt.Errorf("peer responded with mismatched infohash")
	}
	return nil
}

func (c *PeerClient) receiveBitfield() error {
	msg, err := c.Read()
	if err != nil {
		return err
	}
	if msg == nil {
		return fmt.Errorf("peer sent keepalive while bitfield expected")
	}
	if msg.ID != MsgBitfield {
		return fmt.Errorf("expected bitfield, got message id %d", msg.ID)
	}
	c.Bitfield = msg.Payload
	return nil
}

func (c *PeerClient) Read() (*Message, error) {
	return ReadMessage(c.Conn)
}

func (c *PeerClient) sendMessage(m *Message) error {
	_, err := c.Conn.Write(m.Serialize())
	return err
}

func (c *PeerClient) SendInterested() error {
	return c.sendMessage(BuildInterested())
}

func (c *PeerClient) SendUnchoke() error {
	return c.sendMessage(BuildUnchoke())
}

func (c *PeerClient) SendRequest(index, begin, length int) error {
	return c.sendMessage(BuildRequest(index, begin, length))
}

func (c *PeerClient) SendHave(index int) error {
	return c.sendMessage(BuildHave(index))
}

func (c *PeerClient) Close() error {
	if c.Conn == nil {
		return nil
	}
	return c.Conn.Close()
}

func (c *PeerClient) withDeadline(d time.Duration, fn func() error) error {
	if err := c.Conn.SetDeadline(time.Now().Add(d)); err != nil {
		return err
	}
	defer c.Conn.SetDeadline(time.Time{})
	return fn()
}

func (c *PeerClient) readExactly(r io.Reader, p []byte) error {
	_, err := io.ReadFull(r, p)
	return err
}
