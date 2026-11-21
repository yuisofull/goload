// Adapted from watermill-amqp v2.1.3, pkg/amqp/channelprovider.go. License notice in doc.go.

package rabbitmq

import (
	"context"
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// confirmChannel is the publisher's AMQP channel, in confirm mode and reopened whenever the broker closes it.
// Watermill pools these; the dispatch publisher holds one, so confirms and returns arrive in publish order.
type confirmChannel struct {
	conn *connectionWrapper

	amqpConn    amqpConnection
	amqpChan    amqpChannel
	closedChan  chan *amqp.Error
	confirmChan chan amqp.Confirmation
	returnChan  chan amqp.Return

	bindingsPrepared bool
}

func (c *confirmChannel) openAMQPChannel(amqpConn amqpConnection) error {
	amqpChan, err := amqpConn.Channel()
	if err != nil {
		return fmt.Errorf("create AMQP channel: %w", err)
	}

	closedChan := amqpChan.NotifyClose(make(chan *amqp.Error, 1))

	if err = amqpChan.Confirm(false); err != nil {
		return fmt.Errorf("confirm AMQP channel: %w", err)
	}

	c.amqpConn = amqpConn
	c.amqpChan = amqpChan
	c.closedChan = closedChan
	c.confirmChan = amqpChan.NotifyPublish(make(chan amqp.Confirmation, 1))
	c.returnChan = amqpChan.NotifyReturn(make(chan amqp.Return, 1))
	c.bindingsPrepared = false

	return nil
}

func (c *confirmChannel) validate(conn amqpConnection) error {
	if c.amqpChan == nil {
		return c.openAMQPChannel(conn)
	}

	select {
	case reason := <-c.closedChan:
		c.amqpChan = nil
		c.conn.logger.Info(
			"AMQP channel was closed. Opening new channel.",
			logFields{"close-error": closeReason(reason)},
		)

		return c.openAMQPChannel(conn)
	default:
		return nil
	}
}

// publish sends msg as mandatory and waits for the broker's verdict on it.
func (c *confirmChannel) publish(ctx context.Context, exchangeName, routingKey string, msg amqp.Publishing) error {
	if err := c.amqpChan.PublishWithContext(ctx, exchangeName, routingKey, true, false, msg); err != nil {
		c.abort(err)
		return fmt.Errorf("cannot publish msg: %w", err)
	}

	err := c.delivered(ctx)
	switch {
	case err == nil:
	case errors.Is(err, ErrNoConfirm):
		c.abort(err)
	default:
		// The broker refused the message; next time make sure the topology is still there.
		c.bindingsPrepared = false
	}

	return err
}

// delivered waits until the broker confirms the publishing, or ctx gives up on it.
func (c *confirmChannel) delivered(ctx context.Context) error {
	select {
	case confirmed, open := <-c.confirmChan:
		if !open {
			return fmt.Errorf("%w: channel closed: %w", ErrNoConfirm, c.closeError())
		}
		if !confirmed.Ack {
			return ErrNacked
		}
		return c.returned()
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ErrNoConfirm, ctx.Err())
	}
}

// basic.return precedes the confirm and publishes are serialized, so a buffered return is this message's.
func (c *confirmChannel) returned() error {
	select {
	case bounced, open := <-c.returnChan:
		if open {
			return fmt.Errorf("%w: %d %s", ErrUnroutable, bounced.ReplyCode, bounced.ReplyText)
		}
	default:
	}

	return nil
}

func (c *confirmChannel) closeError() error {
	select {
	case reason := <-c.closedChan:
		return closeReason(reason)
	default:
		return amqp.ErrClosed
	}
}

// abort drops the whole connection: a late confirm must not reach the next publish,
// and a silent broker never answers a channel close.
func (c *confirmChannel) abort(reason error) {
	c.conn.abort(c.amqpConn, reason)
	c.amqpChan = nil
}
