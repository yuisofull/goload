package bittorrent

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveTorrentSourceFromMagnet(t *testing.T) {
	u, err := url.Parse("magnet:?xt=urn:btih:abc&as=https%3A%2F%2Fa.example%2Ft.torrent&xs=https%3A%2F%2Fb.example%2Ft.torrent")
	if err != nil {
		t.Fatalf("parse magnet: %v", err)
	}

	got := ResolveTorrentSourceFromMagnet(u)
	assert.Equal(t, "https://b.example/t.torrent", got)
}

func TestResolveTorrentSourceFromMagnet_FallbackAs(t *testing.T) {
	u, err := url.Parse("magnet:?xt=urn:btih:abc&as=https%3A%2F%2Fa.example%2Ft.torrent")
	if err != nil {
		t.Fatalf("parse magnet: %v", err)
	}

	got := ResolveTorrentSourceFromMagnet(u)
	assert.Equal(t, "https://a.example/t.torrent", got)
}

func TestResolveTorrentSourceFromMagnet_None(t *testing.T) {
	u, err := url.Parse("magnet:?xt=urn:btih:abc")
	if err != nil {
		t.Fatalf("parse magnet: %v", err)
	}

	got := ResolveTorrentSourceFromMagnet(u)
	assert.Equal(t, "", got)
}
