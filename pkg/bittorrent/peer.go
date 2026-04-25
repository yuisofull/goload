package bittorrent

import (
	"encoding/binary"
	"fmt"
	"net"
)

// Peer encodes peer endpoint information from tracker responses.
type Peer struct {
	IP   net.IP
	Port uint16
}

func (p Peer) String() string {
	return net.JoinHostPort(p.IP.String(), fmt.Sprintf("%d", p.Port))
}

// UnmarshalPeers parses tracker compact peer encoding (6 bytes per peer).
func UnmarshalPeers(peersBin []byte) ([]Peer, error) {
	const peerSize = 6
	if len(peersBin)%peerSize != 0 {
		return nil, fmt.Errorf("received malformed peers list")
	}
	numPeers := len(peersBin) / peerSize
	peers := make([]Peer, numPeers)
	for i := 0; i < numPeers; i++ {
		offset := i * peerSize
		peers[i] = Peer{
			IP:   net.IP(peersBin[offset : offset+4]),
			Port: binary.BigEndian.Uint16(peersBin[offset+4 : offset+6]),
		}
	}
	return peers, nil
}
