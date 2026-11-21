package rabbitmq

import (
	"context"
	"slices"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

type reaction func(ch *fakeChannel, tag uint64)

type fakeBroker struct {
	mu          sync.Mutex
	connections []*fakeConnection
	refusal     error
	beforeDial  func()
	onPublish   reaction
	silent      chan struct{}
}

func (b *fakeBroker) dial(string) (amqpConnection, error) {
	if hold := b.dialHook(); hold != nil {
		hold()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.refusal != nil {
		return nil, b.refusal
	}

	conn := &fakeConnection{broker: b, done: make(chan struct{})}
	b.connections = append(b.connections, conn)
	return conn, nil
}

func (b *fakeBroker) dialHook() func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.beforeDial
}

func (b *fakeBroker) holdDials(hold func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.beforeDial = hold
}

func (b *fakeBroker) refuse(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refusal = err
}

func (b *fakeBroker) accept() {
	b.refuse(nil)
}

func (b *fakeBroker) react(onPublish reaction) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onPublish = onPublish
}

func (b *fakeBroker) reaction() reaction {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.onPublish == nil {
		return (*fakeChannel).ack
	}
	return b.onPublish
}

func (b *fakeBroker) isSilent() chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.silent
}

func (b *fakeBroker) dials() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.connections)
}

func (b *fakeBroker) connection(index int) *fakeConnection {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connections[index]
}

func (b *fakeBroker) channel(index int) *fakeChannel {
	return b.connection(index).channelAt(0)
}

type fakeConnection struct {
	broker *fakeBroker
	done   chan struct{}

	mu       sync.Mutex
	channels []*fakeChannel
	closes   []chan *amqp.Error
	closed   bool
	aborted  bool
}

func (c *fakeConnection) Channel() (amqpChannel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, amqp.ErrClosed
	}
	ch := newFakeChannel()
	ch.conn = c
	c.channels = append(c.channels, ch)
	return ch, nil
}

func (c *fakeConnection) NotifyClose(closes chan *amqp.Error) chan *amqp.Error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		close(closes)
		return closes
	}
	c.closes = append(c.closes, closes)
	return closes
}

func (c *fakeConnection) Close() error {
	if !c.awaitGoodbye() {
		return amqp.ErrClosed
	}
	c.shutDown(nil, false)
	return nil
}

// awaitGoodbye blocks like a closing handshake: a silent broker answers only once the socket is gone.
func (c *fakeConnection) awaitGoodbye() bool {
	silent := c.broker.isSilent()
	if silent == nil {
		return true
	}
	select {
	case <-silent:
		return true
	case <-c.done:
		return false
	}
}

func (c *fakeConnection) Abort() {
	c.shutDown(&amqp.Error{Code: amqp.FrameError, Reason: "use of closed network connection"}, true)
}

func (c *fakeConnection) kill(reason *amqp.Error) {
	c.shutDown(reason, false)
}

func (c *fakeConnection) shutDown(reason *amqp.Error, aborted bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.closed = true
	c.aborted = aborted
	close(c.done)

	for _, closes := range c.closes {
		if reason != nil {
			closes <- reason
		}
		close(closes)
	}
	for _, ch := range c.channels {
		ch.kill(reason)
	}
}

func (c *fakeConnection) wasAborted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.aborted
}

func (c *fakeConnection) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *fakeConnection) channelAt(index int) *fakeChannel {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.channels[index]
}

func (c *fakeConnection) channelCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.channels)
}

type declaredExchange struct {
	name, kind                            string
	durable, autoDelete, internal, noWait bool
}

type declaredQueue struct {
	name                                   string
	durable, autoDelete, exclusive, noWait bool
	args                                   amqp.Table
}

type binding struct {
	queue, key, exchange string
}

type qos struct {
	count, size int
	global      bool
}

type consumer struct {
	queue                               string
	autoAck, exclusive, noLocal, noWait bool
}

type published struct {
	exchange, key        string
	mandatory, immediate bool
	publishing           amqp.Publishing
}

type settlement struct {
	tag               uint64
	ack               bool
	multiple, requeue bool
}

type fakeChannel struct {
	conn *fakeConnection

	mu          sync.Mutex
	exchanges   []declaredExchange
	queues      []declaredQueue
	bindings    []binding
	confirming  bool
	qos         []qos
	consumers   []consumer
	publishes   []published
	settlements []settlement
	closed      bool

	failQueue  string
	declareErr error

	confirms   chan amqp.Confirmation
	returns    chan amqp.Return
	closes     chan *amqp.Error
	deliveries chan amqp.Delivery
}

func newFakeChannel() *fakeChannel {
	return &fakeChannel{deliveries: make(chan amqp.Delivery, 16)}
}

func (ch *fakeChannel) ExchangeDeclare(
	name, kind string,
	durable, autoDelete, internal, noWait bool,
	_ amqp.Table,
) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.exchanges = append(ch.exchanges, declaredExchange{name, kind, durable, autoDelete, internal, noWait})
	return nil
}

func (ch *fakeChannel) QueueDeclare(
	name string,
	durable, autoDelete, exclusive, noWait bool,
	args amqp.Table,
) (amqp.Queue, error) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if name == ch.failQueue {
		return amqp.Queue{}, ch.declareErr
	}
	ch.queues = append(ch.queues, declaredQueue{name, durable, autoDelete, exclusive, noWait, args})
	return amqp.Queue{Name: name}, nil
}

func (ch *fakeChannel) QueueBind(name, key, exchange string, _ bool, _ amqp.Table) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.bindings = append(ch.bindings, binding{name, key, exchange})
	return nil
}

func (ch *fakeChannel) Confirm(bool) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.confirming = true
	return nil
}

func (ch *fakeChannel) NotifyPublish(confirms chan amqp.Confirmation) chan amqp.Confirmation {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.confirms = confirms
	return confirms
}

func (ch *fakeChannel) NotifyReturn(returns chan amqp.Return) chan amqp.Return {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.returns = returns
	return returns
}

func (ch *fakeChannel) NotifyClose(closes chan *amqp.Error) chan *amqp.Error {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.closes = closes
	return closes
}

func (ch *fakeChannel) PublishWithContext(
	ctx context.Context,
	exchange, key string,
	mandatory, immediate bool,
	publishing amqp.Publishing,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	ch.mu.Lock()
	if ch.closed {
		ch.mu.Unlock()
		return amqp.ErrClosed
	}
	ch.publishes = append(ch.publishes, published{exchange, key, mandatory, immediate, publishing})
	tag := uint64(len(ch.publishes))
	ch.mu.Unlock()

	ch.reaction()(ch, tag)
	return nil
}

func (ch *fakeChannel) reaction() reaction {
	if ch.conn == nil {
		return (*fakeChannel).ack
	}
	return ch.conn.broker.reaction()
}

func (ch *fakeChannel) Qos(count, size int, global bool) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.qos = append(ch.qos, qos{count, size, global})
	return nil
}

func (ch *fakeChannel) Consume(
	queue, _ string,
	autoAck, exclusive, noLocal, noWait bool,
	_ amqp.Table,
) (<-chan amqp.Delivery, error) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.consumers = append(ch.consumers, consumer{queue, autoAck, exclusive, noLocal, noWait})
	return ch.deliveries, nil
}

func (ch *fakeChannel) Close() error {
	if ch.conn != nil && !ch.conn.awaitGoodbye() {
		return amqp.ErrClosed
	}
	ch.kill(nil)
	return nil
}

func (ch *fakeChannel) isClosed() bool {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.closed
}

func (ch *fakeChannel) Ack(tag uint64, multiple bool) error {
	return ch.settle(settlement{tag: tag, ack: true, multiple: multiple})
}

func (ch *fakeChannel) Nack(tag uint64, multiple, requeue bool) error {
	return ch.settle(settlement{tag: tag, multiple: multiple, requeue: requeue})
}

func (ch *fakeChannel) Reject(tag uint64, requeue bool) error {
	return ch.settle(settlement{tag: tag, requeue: requeue})
}

func (ch *fakeChannel) settle(s settlement) error {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if ch.closed {
		return amqp.ErrClosed
	}
	ch.settlements = append(ch.settlements, s)
	return nil
}

func (ch *fakeChannel) ack(tag uint64) {
	ch.confirms <- amqp.Confirmation{DeliveryTag: tag, Ack: true}
}

func (ch *fakeChannel) nack(tag uint64) {
	ch.confirms <- amqp.Confirmation{DeliveryTag: tag, Ack: false}
}

func (ch *fakeChannel) returnThenAck(tag uint64) {
	ch.returns <- amqp.Return{ReplyCode: amqp.NoRoute, ReplyText: "NO_ROUTE"}
	ch.ack(tag)
}

func (ch *fakeChannel) ignore(uint64) {}

func (ch *fakeChannel) deliver(delivery amqp.Delivery) {
	delivery.Acknowledger = ch
	ch.deliveries <- delivery
}

func (ch *fakeChannel) kill(err *amqp.Error) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if ch.closed {
		return
	}
	ch.closed = true

	if ch.closes != nil {
		if err != nil {
			ch.closes <- err
		}
		close(ch.closes)
	}
	if ch.confirms != nil {
		close(ch.confirms)
	}
	if ch.returns != nil {
		close(ch.returns)
	}
	close(ch.deliveries)
}

func (ch *fakeChannel) recordedPublishes() []published {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return slices.Clone(ch.publishes)
}

func (ch *fakeChannel) recordedSettlements() []settlement {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return slices.Clone(ch.settlements)
}

func (ch *fakeChannel) recordedBindings() []binding {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return slices.Clone(ch.bindings)
}

func (ch *fakeChannel) recordedQueues() []declaredQueue {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return slices.Clone(ch.queues)
}

func (ch *fakeChannel) recordedConsumers() []consumer {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return slices.Clone(ch.consumers)
}
