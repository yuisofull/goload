package rabbitmq

import (
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWriteTimeout = 50 * time.Millisecond

func TestDeadlineConn_WriteFailsWhenThePeerStopsReading(t *testing.T) {
	client, peer := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = peer.Close() })
	conn := deadlineConn{Conn: client, writeTimeout: testWriteTimeout}

	started := time.Now()
	_, err := conn.Write([]byte("frame"))

	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	assert.Less(t, time.Since(started), waitFor)
}

func TestDeadlineConn_EachWriteGetsAFreshDeadline(t *testing.T) {
	client, peer := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = peer.Close() })
	go func() { _, _ = io.Copy(io.Discard, peer) }()
	conn := deadlineConn{Conn: client, writeTimeout: testWriteTimeout}

	_, err := conn.Write([]byte("first"))
	require.NoError(t, err)
	time.Sleep(2 * testWriteTimeout)

	_, err = conn.Write([]byte("second"))
	assert.NoError(t, err)
}

func TestDial_GivesUpOnABrokerThatNeverAnswersTheHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go holdConnections(listener)

	started := time.Now()
	_, err = newDialer(testWriteTimeout)("amqp://guest:guest@" + listener.Addr().String() + "/")

	require.Error(t, err)
	assert.Less(t, time.Since(started), waitFor)
}

func holdConnections(listener net.Listener) {
	var held []net.Conn
	defer func() {
		for _, conn := range held {
			_ = conn.Close()
		}
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		held = append(held, conn)
	}
}
