package bittorrent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	bencode "github.com/jackpal/bencode-go"
)

const defaultPeerPort uint16 = 6881

// TorrentFile is flattened metadata parsed from a .torrent file.
type TorrentFile struct {
	Announce    string
	InfoHash    [20]byte
	PieceHashes [][20]byte
	PieceLength int
	Length      int
	Name        string
}

type bencodeInfo struct {
	Pieces      string `bencode:"pieces"`
	PieceLength int    `bencode:"piece length"`
	Length      int    `bencode:"length"`
	Name        string `bencode:"name"`
}

type bencodeTorrent struct {
	Announce string      `bencode:"announce"`
	Info     bencodeInfo `bencode:"info"`
}

type bencodeTrackerResp struct {
	Interval int    `bencode:"interval"`
	Peers    string `bencode:"peers"`
}

// Open parses a .torrent stream into TorrentFile metadata.
func Open(r io.Reader) (*TorrentFile, error) {
	var bt bencodeTorrent
	if err := bencode.Unmarshal(r, &bt); err != nil {
		return nil, err
	}
	out, err := bt.toTorrentFile()
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// OpenFromURL fetches and parses a .torrent from HTTP(S) or local file path.
func OpenFromURL(ctx context.Context, rawURL string, httpClient *http.Client) (*TorrentFile, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse torrent source url %q: %w", rawURL, err)
	}

	if strings.EqualFold(u.Scheme, "magnet") {
		next := ResolveTorrentSourceFromMagnet(u)
		if next == "" {
			return nil, fmt.Errorf("magnet link does not include xs/as torrent source")
		}
		return OpenFromURL(ctx, next, httpClient)
	}

	if u.Scheme == "" || strings.EqualFold(u.Scheme, "file") {
		path := rawURL
		if strings.EqualFold(u.Scheme, "file") {
			path = u.Path
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return Open(f)
	}

	if strings.EqualFold(u.Scheme, "data") {
		return openFromDataURL(rawURL)
	}

	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return nil, fmt.Errorf("unsupported torrent source scheme %q", u.Scheme)
	}

	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch torrent: unexpected status %s", resp.Status)
	}
	return Open(resp.Body)
}

// BuildTrackerURL creates an announce URL with query parameters for this torrent.
func (t *TorrentFile) BuildTrackerURL(peerID [20]byte, port uint16) (string, error) {
	if t.Announce == "" {
		return "", fmt.Errorf("torrent announce url is empty")
	}
	base, err := url.Parse(t.Announce)
	if err != nil {
		return "", err
	}
	params := url.Values{
		"info_hash":  []string{string(t.InfoHash[:])},
		"peer_id":    []string{string(peerID[:])},
		"port":       []string{strconv.Itoa(int(port))},
		"uploaded":   []string{"0"},
		"downloaded": []string{"0"},
		"compact":    []string{"1"},
		"left":       []string{strconv.Itoa(t.Length)},
	}
	base.RawQuery = params.Encode()
	return base.String(), nil
}

// RequestPeers retrieves peers from the HTTP tracker.
func (t *TorrentFile) RequestPeers(ctx context.Context, peerID [20]byte, port uint16, httpClient *http.Client) ([]Peer, error) {
	trackerURL, err := t.BuildTrackerURL(peerID, port)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trackerURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tracker announce failed: %s", resp.Status)
	}
	var tr bencodeTrackerResp
	if err := bencode.Unmarshal(resp.Body, &tr); err != nil {
		return nil, err
	}
	return UnmarshalPeers([]byte(tr.Peers))
}

func (bt bencodeTorrent) toTorrentFile() (TorrentFile, error) {
	if bt.Info.Length <= 0 {
		return TorrentFile{}, fmt.Errorf("unsupported torrent: only single-file torrents are supported")
	}

	infoHash, err := hashInfoDict(bt.Info)
	if err != nil {
		return TorrentFile{}, err
	}
	pieceHashes, err := splitPieceHashes(bt.Info.Pieces)
	if err != nil {
		return TorrentFile{}, err
	}

	return TorrentFile{
		Announce:    bt.Announce,
		InfoHash:    infoHash,
		PieceHashes: pieceHashes,
		PieceLength: bt.Info.PieceLength,
		Length:      bt.Info.Length,
		Name:        bt.Info.Name,
	}, nil
}

func hashInfoDict(info bencodeInfo) ([20]byte, error) {
	var buf bytes.Buffer
	if err := bencode.Marshal(&buf, info); err != nil {
		return [20]byte{}, err
	}
	return sha1.Sum(buf.Bytes()), nil
}

func splitPieceHashes(pieces string) ([][20]byte, error) {
	const hashLen = 20
	raw := []byte(pieces)
	if len(raw)%hashLen != 0 {
		return nil, fmt.Errorf("invalid pieces length %d", len(raw))
	}
	hashes := make([][20]byte, 0, len(raw)/hashLen)
	for i := 0; i < len(raw); i += hashLen {
		var h [20]byte
		copy(h[:], raw[i:i+hashLen])
		hashes = append(hashes, h)
	}
	return hashes, nil
}

// ResolveTorrentSourceFromMagnet extracts an alternate torrent source URL from
// magnet query parameters, preferring xs over as.
func ResolveTorrentSourceFromMagnet(m *url.URL) string {
	if m == nil || !strings.EqualFold(m.Scheme, "magnet") {
		return ""
	}
	q := m.Query()
	if xs := q.Get("xs"); xs != "" {
		return xs
	}
	if as := q.Get("as"); as != "" {
		return as
	}
	return ""
}

func openFromDataURL(rawURL string) (*TorrentFile, error) {
	const marker = ";base64,"
	idx := strings.Index(rawURL, marker)
	if idx < 0 {
		return nil, fmt.Errorf("invalid data url: missing %q marker", marker)
	}
	encoded := rawURL[idx+len(marker):]
	if encoded == "" {
		return nil, fmt.Errorf("invalid data url: empty payload")
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode data url base64 payload: %w", err)
	}
	return Open(bytes.NewReader(payload))
}

// RandomPeerID generates a random 20-byte peer ID.
func RandomPeerID() ([20]byte, error) {
	var id [20]byte
	if _, err := rand.Read(id[:]); err != nil {
		return [20]byte{}, err
	}
	return id, nil
}
