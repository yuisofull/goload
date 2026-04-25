// Package bittorrent implements a small BitTorrent client inspired by
// https://blog.jse.li/posts/torrent/.
//
// Scope:
// - Parse .torrent metadata
// - Request peers from an HTTP tracker
// - Handshake and exchange core peer messages
// - Download verified pieces concurrently
//
// Current limitations:
// - Single-file torrents only
// - HTTP(S) trackers only
// - No DHT/PEX/magnet peer discovery
package bittorrent
