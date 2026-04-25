package bittorrent

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// MaxBlockSize is the largest request block size per protocol recommendation.
	MaxBlockSize = 16384
	// MaxBacklog is the number of in-flight block requests per peer.
	MaxBacklog = 5
)

// Config controls BitTorrent client behavior.
type Config struct {
	Port            uint16
	HandshakeTimeout time.Duration
	MaxPeers        int
	HTTPTimeout     time.Duration
}

// Downloader downloads torrent content described by a TorrentFile.
type Downloader struct {
	cfg        Config
	peerID     [20]byte
	httpClient *http.Client
}

// DownloadResult describes a completed torrent download to a temp file.
type DownloadResult struct {
	Path string
	Name string
	Size int64
	File *os.File
}

// NewDownloader creates a downloader with sane defaults.
func NewDownloader(cfg Config) (*Downloader, error) {
	if cfg.Port == 0 {
		cfg.Port = defaultPeerPort
	}
	if cfg.HandshakeTimeout <= 0 {
		cfg.HandshakeTimeout = 5 * time.Second
	}
	if cfg.MaxPeers <= 0 {
		cfg.MaxPeers = 40
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 20 * time.Second
	}
	peerID, err := RandomPeerID()
	if err != nil {
		return nil, err
	}
	return &Downloader{
		cfg:    cfg,
		peerID: peerID,
		httpClient: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
	}, nil
}

// OpenTorrentFromSource resolves and parses source metadata.
func (d *Downloader) OpenTorrentFromSource(ctx context.Context, source string) (*TorrentFile, error) {
	return OpenFromURL(ctx, source, d.httpClient)
}

// Download fetches the torrent file content into a temporary file.
func (d *Downloader) Download(ctx context.Context, tf *TorrentFile) (*DownloadResult, error) {
	peers, err := tf.RequestPeers(ctx, d.peerID, d.cfg.Port, d.httpClient)
	if err != nil {
		return nil, err
	}
	if len(peers) == 0 {
		return nil, fmt.Errorf("no peers returned from tracker")
	}
	if len(peers) > d.cfg.MaxPeers {
		peers = peers[:d.cfg.MaxPeers]
	}

	workQueue := make(chan *pieceWork, len(tf.PieceHashes))
	results := make(chan *pieceResult)
	for index, hash := range tf.PieceHashes {
		length := tf.CalculatePieceSize(index)
		workQueue <- &pieceWork{index: index, hash: hash, length: length}
	}

	tmp, err := os.CreateTemp("", "goload-bittorrent-*")
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	for _, p := range peers {
		peer := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.startDownloadWorker(ctx, tf, peer, workQueue, results)
		}()
	}

	donePieces := 0
	for donePieces < len(tf.PieceHashes) {
		select {
		case <-ctx.Done():
			close(workQueue)
			wg.Wait()
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return nil, ctx.Err()
		case res := <-results:
			begin, _ := tf.CalculateBoundsForPiece(res.index)
			if _, err := tmp.WriteAt(res.buf, int64(begin)); err != nil {
				close(workQueue)
				wg.Wait()
				_ = tmp.Close()
				_ = os.Remove(tmp.Name())
				return nil, err
			}
			donePieces++
		}
	}

	close(workQueue)
	wg.Wait()

	if _, err := tmp.Seek(0, 0); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, err
	}

	name := tf.Name
	if name == "" {
		name = filepath.Base(tmp.Name())
	}

	return &DownloadResult{Path: tmp.Name(), Name: name, Size: int64(tf.Length), File: tmp}, nil
}

type pieceWork struct {
	index  int
	hash   [20]byte
	length int
}

type pieceResult struct {
	index int
	buf   []byte
}

type pieceProgress struct {
	index      int
	client     *PeerClient
	buf        []byte
	downloaded int
	requested  int
	backlog    int
}

func (d *Downloader) startDownloadWorker(
	ctx context.Context,
	tf *TorrentFile,
	peer Peer,
	workQueue chan *pieceWork,
	results chan *pieceResult,
) {
	c, err := NewPeerClient(ctx, peer, d.peerID, tf.InfoHash, d.cfg.HandshakeTimeout)
	if err != nil {
		return
	}
	defer c.Close()

	if err := c.SendInterested(); err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case pw, ok := <-workQueue:
			if !ok {
				return
			}
			if !c.Bitfield.HasPiece(pw.index) {
				// Put work back for other peers.
				select {
				case workQueue <- pw:
				default:
				}
				continue
			}
			buf, err := attemptDownloadPiece(ctx, c, pw)
			if err != nil {
				// Put work back and stop this worker; peer likely unhealthy.
				select {
				case workQueue <- pw:
				default:
				}
				return
			}
			if err := checkIntegrity(pw, buf); err != nil {
				// Hash mismatch: retry piece on another peer.
				select {
				case workQueue <- pw:
				default:
				}
				continue
			}
			_ = c.SendHave(pw.index)
			select {
			case <-ctx.Done():
				return
			case results <- &pieceResult{index: pw.index, buf: buf}:
			}
		}
	}
}

func attemptDownloadPiece(ctx context.Context, c *PeerClient, pw *pieceWork) ([]byte, error) {
	state := pieceProgress{
		index:  pw.index,
		client: c,
		buf:    make([]byte, pw.length),
	}
	if err := c.Conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	defer c.Conn.SetDeadline(time.Time{})

	for state.downloaded < pw.length {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if !state.client.Choked {
			for state.backlog < MaxBacklog && state.requested < pw.length {
				blockSize := MaxBlockSize
				if pw.length-state.requested < blockSize {
					blockSize = pw.length - state.requested
				}
				if err := c.SendRequest(pw.index, state.requested, blockSize); err != nil {
					return nil, err
				}
				state.backlog++
				state.requested += blockSize
			}
		}

		msg, err := c.Read()
		if err != nil {
			return nil, err
		}
		if msg == nil {
			continue
		}
		switch msg.ID {
		case MsgUnchoke:
			state.client.Choked = false
		case MsgChoke:
			state.client.Choked = true
		case MsgHave:
			index, err := ParseHave(msg)
			if err == nil {
				state.client.Bitfield.SetPiece(index)
			}
		case MsgPiece:
			n, err := ParsePiece(state.index, state.buf, msg)
			if err != nil {
				return nil, err
			}
			state.downloaded += n
			if state.backlog > 0 {
				state.backlog--
			}
		}
	}
	return state.buf, nil
}

func checkIntegrity(pw *pieceWork, buf []byte) error {
	hash := sha1.Sum(buf)
	if hash != pw.hash {
		return fmt.Errorf("piece %d failed integrity check", pw.index)
	}
	return nil
}

func (t *TorrentFile) CalculateBoundsForPiece(index int) (begin, end int) {
	begin = index * t.PieceLength
	end = begin + t.CalculatePieceSize(index)
	return begin, end
}

func (t *TorrentFile) CalculatePieceSize(index int) int {
	start := index * t.PieceLength
	end := start + t.PieceLength
	if end > t.Length {
		end = t.Length
	}
	return end - start
}
