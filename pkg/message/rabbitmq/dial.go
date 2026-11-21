package rabbitmq

import (
	"context"
	"errors"
	"net"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const subscriberWriteTimeout = 30 * time.Second

type amqpChannel interface {
	topologyDeclarer
	Confirm(noWait bool) error
	NotifyPublish(confirms chan amqp.Confirmation) chan amqp.Confirmation
	NotifyReturn(returns chan amqp.Return) chan amqp.Return
	NotifyClose(closes chan *amqp.Error) chan *amqp.Error
	PublishWithContext(
		ctx context.Context,
		exchange, key string,
		mandatory, immediate bool,
		msg amqp.Publishing,
	) error
	Qos(prefetchCount, prefetchSize int, global bool) error
	Consume(
		queue, consumer string,
		autoAck, exclusive, noLocal, noWait bool,
		args amqp.Table,
	) (<-chan amqp.Delivery, error)
	Close() error
}

type amqpConnection interface {
	Channel() (amqpChannel, error)
	NotifyClose(closes chan *amqp.Error) chan *amqp.Error
	Close() error
	Abort()
}

type dialFunc func(url string) (amqpConnection, error)

type connection struct {
	conn   *amqp.Connection
	socket net.Conn
}

// newDialer bounds the dial and handshake by timeout, then every write.
func newDialer(timeout time.Duration) dialFunc {
	return func(url string) (amqpConnection, error) {
		var socket net.Conn

		conn, err := amqp.DialConfig(url, amqp.Config{
			Dial: func(network, addr string) (net.Conn, error) {
				dialed, err := amqp.DefaultDial(timeout)(network, addr)
				if err != nil {
					return nil, err
				}
				socket = dialed
				return deadlineConn{Conn: dialed, writeTimeout: timeout}, nil
			},
		})
		if err != nil {
			return nil, err
		}
		return &connection{conn: conn, socket: socket}, nil
	}
}

func (c *connection) Channel() (amqpChannel, error) {
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, err
	}
	return ch, nil
}

func (c *connection) NotifyClose(closes chan *amqp.Error) chan *amqp.Error {
	return c.conn.NotifyClose(closes)
}

func (c *connection) Close() error {
	err := c.conn.Close()
	if errors.Is(err, amqp.ErrClosed) {
		return nil
	}
	return err
}

// Abort drops the socket without the closing handshake.
func (c *connection) Abort() {
	_ = c.socket.Close()
}

// amqp091 never times out a write and holds its shutdown locks while writing: a stuck one hangs everything.
type deadlineConn struct {
	net.Conn

	writeTimeout time.Duration
}

func (c deadlineConn) Write(p []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}
