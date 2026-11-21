// Adapted from watermill-amqp v2.1.3, pkg/amqp/connection.go. License notice in doc.go.

package rabbitmq

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kit/log"
	amqp "github.com/rabbitmq/amqp091-go"
)

const closeTimeout = 5 * time.Second

var (
	errNotConnected = errors.New("not connected to AMQP")
	errCloseTimeout = errors.New("broker did not answer the close in time")
)

// connectionWrapper manages an AMQP connection and redials it until closed.
type connectionWrapper struct {
	url            string
	reconnectDelay time.Duration
	closeTimeout   time.Duration
	dial           dialFunc
	logger         loggerAdapter

	amqpConnectionLock sync.Mutex
	amqpConnection     amqpConnection // nil while disconnected
	disconnectReason   error

	closing chan struct{}
	closed  atomic.Bool
}

func newConnectionWrapper(config Config, dial dialFunc) *connectionWrapper {
	return &connectionWrapper{
		url:            config.URL,
		reconnectDelay: config.ReconnectDelay,
		closeTimeout:   closeTimeout,
		dial:           dial,
		logger:         loggerAdapter{log.NewNopLogger()},
		closing:        make(chan struct{}),
	}
}

func (c *connectionWrapper) open() error {
	conn, err := c.connect()
	if err != nil {
		return err
	}

	go c.handleConnectionClose(conn)

	return nil
}

func (c *connectionWrapper) close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	close(c.closing)

	conn := c.release()
	if conn == nil {
		return nil
	}

	return c.closeBounded(conn, conn.Close)
}

// release takes the live connection, if any, away from the wrapper.
func (c *connectionWrapper) release() amqpConnection {
	c.amqpConnectionLock.Lock()
	defer c.amqpConnectionLock.Unlock()

	conn := c.amqpConnection
	if conn != nil {
		c.amqpConnection = nil
		c.disconnectReason = amqp.ErrClosed
	}

	return conn
}

// closeBounded aborts conn when the broker leaves a closing handshake unanswered.
func (c *connectionWrapper) closeBounded(conn amqpConnection, handshake func() error) error {
	giveUp := time.AfterFunc(c.closeTimeout, func() { c.abort(conn, errCloseTimeout) })
	defer giveUp.Stop()

	return handshake()
}

// abort drops conn without a closing handshake.
func (c *connectionWrapper) abort(conn amqpConnection, reason error) {
	c.disconnect(conn, reason)
	conn.Abort()
}

// withDeadline runs fn, aborting the live connection if it has not finished in
// time. A broker that stalls mid-operation (channel open, topology declare, a
// confirm) therefore cannot block past timeout: aborting the socket unblocks
// the stuck call and the wrapper reconnects.
func (c *connectionWrapper) withDeadline(timeout time.Duration, fn func(amqpConnection) error) error {
	// fn runs on this connection and the timeout aborts this one, so setup and
	// its deadline share a socket even if a reconnect replaces the live one.
	conn, err := c.connection()
	if err != nil {
		return err
	}
	giveUp := time.AfterFunc(timeout, func() {
		c.abort(conn, fmt.Errorf("broker did not respond within %s", timeout))
	})
	defer giveUp.Stop()
	return fn(conn)
}

func (c *connectionWrapper) connect() (amqpConnection, error) {
	conn, err := c.dial(c.url)

	c.amqpConnectionLock.Lock()
	defer c.amqpConnectionLock.Unlock()

	switch {
	case err != nil:
		c.disconnectReason = fmt.Errorf("cannot connect to RabbitMQ: %w", err)
		return nil, c.disconnectReason
	case c.closed.Load():
		// close ran during the dial and will never see conn.
		conn.Abort()
		return nil, amqp.ErrClosed
	}

	c.amqpConnection = conn

	return conn, nil
}

// connection returns the live connection, or why there is none.
func (c *connectionWrapper) connection() (amqpConnection, error) {
	c.amqpConnectionLock.Lock()
	defer c.amqpConnectionLock.Unlock()

	if c.amqpConnection == nil {
		return nil, fmt.Errorf("%w: %w", errNotConnected, c.disconnectReason)
	}

	return c.amqpConnection, nil
}

func (c *connectionWrapper) disconnect(conn amqpConnection, reason error) {
	c.amqpConnectionLock.Lock()
	defer c.amqpConnectionLock.Unlock()

	if c.amqpConnection == conn {
		c.amqpConnection = nil
		c.disconnectReason = reason
	}
}

func (c *connectionWrapper) handleConnectionClose(conn amqpConnection) {
	for {
		notifyCloseConnection := conn.NotifyClose(make(chan *amqp.Error, 1))

		select {
		case <-c.closing:
			return
		case reason := <-notifyCloseConnection:
			if c.closed.Load() {
				return
			}
			err := closeReason(reason)
			c.disconnect(conn, err)
			c.logger.Error("Received close notification from AMQP, reconnecting", err, nil)
		}

		var reconnected bool
		if conn, reconnected = c.reconnect(); !reconnected {
			return
		}
	}
}

// reconnect redials every reconnectDelay until it succeeds or the wrapper closes.
func (c *connectionWrapper) reconnect() (amqpConnection, bool) {
	for !c.closed.Load() {
		conn, err := c.connect()
		if err == nil {
			c.logger.Info("Reconnected to AMQP", nil)
			return conn, true
		}
		if !c.closed.Load() {
			c.logger.Error("Cannot reconnect to AMQP, retrying", err, nil)
		}

		select {
		case <-c.closing:
		case <-time.After(c.reconnectDelay):
		}
	}

	return nil, false
}

// closeReason guards against the nil *amqp.Error a graceful close notifies with.
func closeReason(reason *amqp.Error) error {
	if reason == nil {
		return amqp.ErrClosed
	}
	return reason
}
