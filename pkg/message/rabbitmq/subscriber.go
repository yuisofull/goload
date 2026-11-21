// Adapted from watermill-amqp v2.1.3, pkg/amqp/subscriber.go. License notice in doc.go.

package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/yuisofull/goload/pkg/message"
)

type ErrorHandler func(context.Context, error)

var ErrSubscriberClosed = errors.New("subscriber is closed")

type Subscriber struct {
	conn         *connectionWrapper
	config       Config
	unmarshaler  Unmarshaler
	errorHandler ErrorHandler
	topic        topicClaim

	closing             context.Context
	closeSubscriber     context.CancelFunc
	subscribeLock       sync.Mutex
	subscriberWaitGroup sync.WaitGroup
}

type SubscriberOption func(*Subscriber)

func WithErrorHandler(handler ErrorHandler) SubscriberOption {
	return func(s *Subscriber) {
		s.errorHandler = handler
	}
}

func WithLog(logger log.Logger) SubscriberOption {
	return func(s *Subscriber) {
		if logger != nil {
			s.conn.logger = loggerAdapter{logger}
		}
	}
}

func WithUnmarshaler(u Unmarshaler) SubscriberOption {
	return func(s *Subscriber) {
		s.unmarshaler = u
	}
}

func NewSubscriber(cfg *Config, opts ...SubscriberOption) (*Subscriber, error) {
	config, err := resolveConfig(cfg)
	if err != nil {
		return nil, err
	}

	closing, closeSubscriber := context.WithCancel(context.Background())
	s := &Subscriber{
		conn:            newConnectionWrapper(config, newDialer(subscriberWriteTimeout)),
		config:          config,
		unmarshaler:     DefaultMarshaler{},
		errorHandler:    func(context.Context, error) {},
		closing:         closing,
		closeSubscriber: closeSubscriber,
	}
	for _, opt := range opts {
		opt(s)
	}

	if err = s.conn.open(); err != nil {
		closeSubscriber()
		return nil, err
	}

	return s, nil
}

// Subscribe consumes the work queue. Up to Prefetch messages are out at a time,
// each acked on the broker only once the consumer acks it.
func (s *Subscriber) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	if s.closing.Err() != nil {
		return nil, ErrSubscriberClosed
	}
	if err := s.topic.claim(topic, s.config.Queue); err != nil {
		return nil, err
	}

	out := make(chan *message.Message)

	sub, err := s.openSubscription(ctx, topic, out)
	if err != nil {
		return nil, fmt.Errorf("cannot subscribe to %s: %w", topic, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	unlink := context.AfterFunc(s.closing, cancel)

	started := s.start(func() {
		defer func() {
			unlink()
			cancel()
			close(out)
			s.conn.logger.Info("Stopped consuming from AMQP channel", sub.logFields)
		}()

		s.runSubscriber(ctx, topic, sub)
	})
	if !started {
		unlink()
		cancel()
		sub.close()

		return nil, ErrSubscriberClosed
	}

	s.conn.logger.Info("Starting consuming from AMQP channel", sub.logFields)

	return out, nil
}

func (s *Subscriber) SubscribeInitialize(topic string) error {
	if s.closing.Err() != nil {
		return ErrSubscriberClosed
	}
	if err := s.topic.claim(topic, s.config.Queue); err != nil {
		return err
	}

	return s.prepareConsume(topic)
}

func (s *Subscriber) Close() error {
	s.subscribeLock.Lock()
	s.closeSubscriber()
	s.subscribeLock.Unlock()

	s.subscriberWaitGroup.Wait()

	return s.conn.close()
}

func (s *Subscriber) start(subscription func()) bool {
	s.subscribeLock.Lock()
	defer s.subscribeLock.Unlock()

	if s.closing.Err() != nil {
		return false
	}
	s.subscriberWaitGroup.Go(subscription)

	return true
}

func (s *Subscriber) prepareConsume(topic string) error {
	sub, err := s.openSubscribeChannel(topic, nil)
	if err != nil {
		return err
	}
	defer sub.close()

	return buildTopology(sub.channel, s.config, topic)
}

// runSubscriber consumes until ctx is done, on a new channel whenever the current one is lost.
func (s *Subscriber) runSubscriber(ctx context.Context, topic string, sub *subscription) {
	for {
		sub.processMessages(ctx)
		sub.close()

		if sub = s.resubscribe(ctx, topic, sub.out); sub == nil {
			return
		}
	}
}

func (s *Subscriber) resubscribe(ctx context.Context, topic string, out chan<- *message.Message) *subscription {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.config.ReconnectDelay):
		}

		sub, err := s.openSubscription(ctx, topic, out)
		if err == nil {
			s.conn.logger.Info("Resumed consuming from AMQP channel", sub.logFields)
			return sub
		}
		s.report(ctx, fmt.Errorf("cannot resubscribe to %s: %w", topic, err))
	}
}

func (s *Subscriber) openSubscription(
	ctx context.Context,
	topic string,
	out chan<- *message.Message,
) (*subscription, error) {
	sub, err := s.openSubscribeChannel(topic, out)
	if err != nil {
		return nil, err
	}

	if err = sub.consume(ctx, topic); err != nil {
		sub.close()
		return nil, err
	}

	return sub, nil
}

func (s *Subscriber) openSubscribeChannel(topic string, out chan<- *message.Message) (*subscription, error) {
	amqpConn, err := s.conn.connection()
	if err != nil {
		return nil, err
	}

	channel, err := amqpConn.Channel()
	if err != nil {
		return nil, fmt.Errorf("cannot open channel: %w", err)
	}

	sub := &subscription{
		subscriber: s,
		out:        out,
		logFields: logFields{
			"topic":              topic,
			"amqp_queue_name":    s.config.Queue,
			"amqp_exchange_name": s.config.Exchange,
		},
		amqpConn: amqpConn,
		channel:  channel,
		lost:     make(chan struct{}),
	}

	if err = channel.Qos(s.config.Prefetch, 0, false); err != nil {
		sub.close()
		return nil, fmt.Errorf("failed to set channel Qos: %w", err)
	}

	return sub, nil
}

func (s *Subscriber) report(ctx context.Context, err error) {
	s.conn.logger.Error("RabbitMQ subscriber error", err, logFields{"queue": s.config.Queue})
	s.errorHandler(ctx, err)
}

type subscription struct {
	subscriber *Subscriber
	out        chan<- *message.Message
	logFields  logFields

	amqpConn amqpConnection
	channel  amqpChannel
	amqpMsgs <-chan amqp.Delivery
	// lost is closed once the channel is gone: its deliveries can no longer be settled.
	lost chan struct{}
}

func (s *subscription) consume(ctx context.Context, topic string) error {
	if err := buildTopology(s.channel, s.subscriber.config, topic); err != nil {
		return err
	}

	notifyCloseChannel := s.channel.NotifyClose(make(chan *amqp.Error, 1))

	amqpMsgs, err := s.createConsumer()
	if err != nil {
		return err
	}
	s.amqpMsgs = amqpMsgs

	go s.watch(ctx, notifyCloseChannel)

	return nil
}

func (s *subscription) createConsumer() (<-chan amqp.Delivery, error) {
	amqpMsgs, err := s.channel.Consume(
		s.subscriber.config.Queue,
		"",
		false, // autoAck must be false: the delivery is acked when the message is
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("cannot consume from channel: %w", err)
	}

	return amqpMsgs, nil
}

func (s *subscription) watch(ctx context.Context, notifyCloseChannel <-chan *amqp.Error) {
	if reason := <-notifyCloseChannel; reason != nil {
		s.subscriber.report(ctx, fmt.Errorf("channel closed: %w", reason))
	}
	close(s.lost)
}

func (s *subscription) processMessages(ctx context.Context) {
	for {
		select {
		case amqpMsg, open := <-s.amqpMsgs:
			// Closed by the lost channel or a consumer cancel; a zero delivery must not reach the consumer.
			if !open {
				return
			}
			s.processMessage(ctx, amqpMsg)
		case <-ctx.Done():
			return
		}
	}
}

// Watermill waits here for the ack before taking the next delivery. Dispatch runs downloads
// concurrently, so the wait moves to awaitSettle and Prefetch alone bounds what is out.
func (s *subscription) processMessage(ctx context.Context, amqpMsg amqp.Delivery) {
	msg, err := s.subscriber.unmarshaler.Unmarshal(amqpMsg)
	if err != nil {
		s.subscriber.report(ctx, fmt.Errorf("cannot unmarshal message %s: %w", amqpMsg.MessageId, err))
		s.deadLetter(ctx, amqpMsg)

		return
	}

	msgCtx, cancelCtx := context.WithCancel(setRedeliveredToCtx(ctx, amqpMsg.Redelivered))
	msg.SetContext(msgCtx)

	select {
	case s.out <- msg:
	case <-ctx.Done():
		cancelCtx()
		return
	case <-s.lost:
		cancelCtx()
		return
	}

	s.subscriber.subscriberWaitGroup.Go(func() {
		defer cancelCtx()
		s.awaitSettle(ctx, amqpMsg, msg)
	})
}

func (s *subscription) awaitSettle(ctx context.Context, amqpMsg amqp.Delivery, msg *message.Message) {
	select {
	case <-msg.Acked():
		s.reportUnsettled(ctx, amqpMsg, amqpMsg.Ack(false))
	case <-msg.Nacked():
		// On shutdown the closing channel requeues unacked deliveries itself.
		if ctx.Err() == nil {
			s.reportUnsettled(ctx, amqpMsg, amqpMsg.Nack(false, true))
		}
	case <-s.lost:
	case <-ctx.Done():
	}
}

// requeue=false: no retry can fix a delivery that does not unmarshal.
func (s *subscription) deadLetter(ctx context.Context, amqpMsg amqp.Delivery) {
	s.reportUnsettled(ctx, amqpMsg, amqpMsg.Nack(false, false))
}

func (s *subscription) reportUnsettled(ctx context.Context, amqpMsg amqp.Delivery, err error) {
	if err != nil {
		s.subscriber.report(
			ctx,
			fmt.Errorf("cannot settle message %s, it will be redelivered: %w", amqpMsg.MessageId, err),
		)
	}
}

// close gives the channel back; the broker requeues what it still holds unacked.
func (s *subscription) close() {
	err := s.subscriber.conn.closeBounded(s.amqpConn, s.channel.Close)
	if err != nil && !errors.Is(err, amqp.ErrClosed) {
		s.subscriber.conn.logger.Error("Failed to close channel", err, s.logFields)
	}
}
