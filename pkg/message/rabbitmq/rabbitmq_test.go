package rabbitmq_test

import (
	"errors"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/pkg/message"
	"github.com/yuisofull/goload/pkg/message/rabbitmq"
)

const (
	topic = "task.created"

	receiveTimeout = 15 * time.Second
	quiet          = time.Second
	confirmTimeout = 2 * time.Second
)

func brokerURL(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		t.Skip("skipping integration test: RABBITMQ_URL is not set")
	}
	return url
}

func newConfig(t *testing.T) *rabbitmq.Config {
	t.Helper()

	suffix := uuid.NewString()
	cfg := &rabbitmq.Config{
		URL:                brokerURL(t),
		Exchange:           "test.dispatch." + suffix,
		Queue:              "test.task." + suffix,
		DeadLetterExchange: "test.dispatch.dlx." + suffix,
		DeadLetterQueue:    "test.task.dlq." + suffix,
		Prefetch:           2,
		ConfirmTimeout:     10 * time.Second,
		ReconnectDelay:     100 * time.Millisecond,
	}
	t.Cleanup(func() { deleteTopology(t, cfg) })
	return cfg
}

func rawChannel(t *testing.T, url string) *amqp.Channel {
	t.Helper()

	conn, err := amqp.Dial(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ch, err := conn.Channel()
	require.NoError(t, err)
	return ch
}

func deleteTopology(t *testing.T, cfg *rabbitmq.Config) {
	t.Helper()

	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		t.Logf("warning: failed to connect to delete the test topology: %v", err)
		return
	}
	defer conn.Close()

	if err = deleteQueuesAndExchanges(conn, cfg); err != nil {
		t.Logf("warning: failed to delete the test topology: %v", err)
	}
}

func deleteQueuesAndExchanges(conn *amqp.Connection, cfg *rabbitmq.Config) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	for _, queue := range []string{cfg.Queue, cfg.DeadLetterQueue} {
		if _, err = ch.QueueDelete(queue, false, false, false); err != nil {
			return err
		}
	}
	for _, exchange := range []string{cfg.Exchange, cfg.DeadLetterExchange} {
		if err = ch.ExchangeDelete(exchange, false, false); err != nil {
			return err
		}
	}
	return nil
}

func newPublisher(t *testing.T, cfg *rabbitmq.Config) *rabbitmq.Publisher {
	t.Helper()
	pub, err := rabbitmq.NewPublisher(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pub.Close() })
	return pub
}

func newSubscriber(t *testing.T, cfg *rabbitmq.Config, opts ...rabbitmq.SubscriberOption) *rabbitmq.Subscriber {
	t.Helper()
	sub, err := rabbitmq.NewSubscriber(cfg, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })
	return sub
}

func subscribe(t *testing.T, sub *rabbitmq.Subscriber) <-chan *message.Message {
	t.Helper()
	messages, err := sub.Subscribe(t.Context(), topic)
	require.NoError(t, err)
	return messages
}

func receive(t *testing.T, messages <-chan *message.Message) *message.Message {
	t.Helper()
	select {
	case msg, ok := <-messages:
		require.True(t, ok, "channel closed unexpectedly")
		return msg
	case <-time.After(receiveTimeout):
		t.Fatal("timed out waiting for message")
		return nil
	}
}

func requireNoDelivery(t *testing.T, messages <-chan *message.Message) {
	t.Helper()
	select {
	case msg := <-messages:
		t.Fatalf("unexpected delivery of message %s", msg.UUID)
	case <-time.After(quiet):
	}
}

func redelivered(t *testing.T, msg *message.Message) bool {
	t.Helper()
	flag, ok := rabbitmq.MessageRedeliveredFromCtx(msg.Context())
	require.True(t, ok, "expected the redelivered flag in the message context")
	return flag
}

func TestPublishSubscribe(t *testing.T) {
	cfg := newConfig(t)
	messages := subscribe(t, newSubscriber(t, cfg))

	want := message.NewMessage("test-uuid-1", []byte(`{"hello":"world"}`))
	want.Metadata.Set("eventType", "TaskCreated")
	want.Metadata.Set("taskID", "42")
	require.NoError(t, newPublisher(t, cfg).Publish(topic, want))

	got := receive(t, messages)
	assert.Equal(t, want.UUID, got.UUID)
	assert.Equal(t, want.Payload, got.Payload)
	assert.Equal(t, "TaskCreated", got.Metadata.Get("eventType"))
	assert.Equal(t, "42", got.Metadata.Get("taskID"))
	assert.False(t, redelivered(t, got))
	got.Ack()
}

func TestPublishBeforeSubscribe(t *testing.T) {
	cfg := newConfig(t)
	require.NoError(t, newPublisher(t, cfg).Publish(topic, message.NewMessage("early-uuid", []byte("early"))))

	got := receive(t, subscribe(t, newSubscriber(t, cfg)))

	assert.Equal(t, "early-uuid", got.UUID)
	got.Ack()
}

func TestPrefetchBoundsUnackedDeliveries(t *testing.T) {
	cfg := newConfig(t)
	cfg.Prefetch = 1
	messages := subscribe(t, newSubscriber(t, cfg))

	require.NoError(t, newPublisher(t, cfg).Publish(topic,
		message.NewMessage("uuid-1", []byte("payload-1")),
		message.NewMessage("uuid-2", []byte("payload-2")),
	))

	first := receive(t, messages)
	assert.Equal(t, "uuid-1", first.UUID)
	requireNoDelivery(t, messages)

	first.Ack()

	second := receive(t, messages)
	assert.Equal(t, "uuid-2", second.UUID)
	second.Ack()
}

func TestUnackedDeliveryIsRedelivered(t *testing.T) {
	cfg := newConfig(t)
	cfg.Prefetch = 1
	pub := newPublisher(t, cfg)
	require.NoError(t, pub.Publish(topic,
		message.NewMessage("acked-uuid", []byte("acked")),
		message.NewMessage("unacked-uuid", []byte("unacked")),
	))

	worker := newSubscriber(t, cfg)
	messages := subscribe(t, worker)
	receive(t, messages).Ack()
	abandoned := receive(t, messages)
	assert.Equal(t, "unacked-uuid", abandoned.UUID)
	require.NoError(t, worker.Close())

	got := receive(t, subscribe(t, newSubscriber(t, cfg)))

	assert.Equal(t, "unacked-uuid", got.UUID)
	assert.True(t, redelivered(t, got))
	got.Ack()
}

func TestNackRedelivers(t *testing.T) {
	cfg := newConfig(t)
	messages := subscribe(t, newSubscriber(t, cfg))
	require.NoError(t, newPublisher(t, cfg).Publish(topic, message.NewMessage("transient-uuid", []byte("transient"))))

	receive(t, messages).Nack()

	retry := receive(t, messages)
	assert.Equal(t, "transient-uuid", retry.UUID)
	assert.True(t, redelivered(t, retry))
	retry.Ack()
	requireNoDelivery(t, messages)
	requireNoDeadLetter(t, cfg)
}

func TestNackDeadLettersAfterTheDeliveryLimit(t *testing.T) {
	cfg := newConfig(t)
	cfg.DeliveryLimit = 2
	messages := subscribe(t, newSubscriber(t, cfg))
	require.NoError(t, newPublisher(t, cfg).Publish(topic, message.NewMessage("poison-uuid", []byte("poison"))))

	for attempt := range cfg.DeliveryLimit + 1 {
		got := receive(t, messages)
		assert.Equal(t, attempt > 0, redelivered(t, got), "attempt %d", attempt)
		got.Nack()
	}

	dead := receiveDeadLetter(t, cfg)
	assert.Equal(t, "poison-uuid", dead.MessageId)
	assert.Equal(t, []byte("poison"), dead.Body)
	assert.Equal(t, "delivery_limit", dead.Headers["x-first-death-reason"])
	assert.Equal(t, cfg.Queue, dead.Headers["x-first-death-queue"])
	requireNoDelivery(t, messages)

	parked, err := rabbitmq.DefaultMarshaler{}.Unmarshal(dead)
	require.NoError(t, err)
	assert.Equal(t, "poison-uuid", parked.UUID)
}

type brokenUnmarshaler struct{}

func (brokenUnmarshaler) Unmarshal(amqp.Delivery) (*message.Message, error) {
	return nil, errors.New("malformed delivery")
}

func TestUnmarshalFailureDeadLettersAtOnce(t *testing.T) {
	cfg := newConfig(t)
	messages := subscribe(t, newSubscriber(t, cfg, rabbitmq.WithUnmarshaler(brokenUnmarshaler{})))
	require.NoError(t, newPublisher(t, cfg).Publish(topic, message.NewMessage("malformed-uuid", []byte("malformed"))))

	dead := receiveDeadLetter(t, cfg)

	assert.Equal(t, "malformed-uuid", dead.MessageId)
	assert.Equal(t, "rejected", dead.Headers["x-first-death-reason"])
	requireNoDelivery(t, messages)
}

func receiveDeadLetter(t *testing.T, cfg *rabbitmq.Config) amqp.Delivery {
	t.Helper()

	ch := rawChannel(t, cfg.URL)
	deadline := time.Now().Add(receiveTimeout)
	for time.Now().Before(deadline) {
		dead, ok, err := ch.Get(cfg.DeadLetterQueue, true)
		require.NoError(t, err)
		if ok {
			return dead
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("message did not reach the dead-letter queue")
	return amqp.Delivery{}
}

func requireNoDeadLetter(t *testing.T, cfg *rabbitmq.Config) {
	t.Helper()

	dead, ok, err := rawChannel(t, cfg.URL).Get(cfg.DeadLetterQueue, true)
	require.NoError(t, err)
	require.False(t, ok, "unexpected dead letter %s", dead.MessageId)
}

type faultyProxy struct {
	listener net.Listener
	broker   string
	stalled  chan struct{}
	released chan struct{}

	mu    sync.Mutex
	conns []net.Conn
}

func newFaultyProxy(t *testing.T, brokerURL string) *faultyProxy {
	t.Helper()

	uri, err := amqp.ParseURI(brokerURL)
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	proxy := &faultyProxy{
		listener: listener,
		broker:   net.JoinHostPort(uri.Host, strconv.Itoa(uri.Port)),
		stalled:  make(chan struct{}),
		released: make(chan struct{}),
	}
	go proxy.accept()
	t.Cleanup(func() {
		close(proxy.released)
		_ = listener.Close()
	})
	return proxy
}

func (p *faultyProxy) url(t *testing.T, brokerURL string) string {
	t.Helper()

	uri, err := amqp.ParseURI(brokerURL)
	require.NoError(t, err)
	host, port, err := net.SplitHostPort(p.listener.Addr().String())
	require.NoError(t, err)
	uri.Host = host
	uri.Port, err = strconv.Atoi(port)
	require.NoError(t, err)
	return uri.String()
}

func (p *faultyProxy) stall() {
	close(p.stalled)
}

// sever drops every proxied connection, as a broker restart would.
func (p *faultyProxy) sever() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = nil
}

func (p *faultyProxy) accept() {
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		broker, err := net.Dial("tcp", p.broker)
		if err != nil {
			_ = client.Close()
			continue
		}

		p.mu.Lock()
		p.conns = append(p.conns, client, broker)
		p.mu.Unlock()

		go p.forward(broker, client)
		go p.forward(client, broker)
	}
}

func (p *faultyProxy) forward(dst, src net.Conn) {
	defer dst.Close()
	defer src.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		select {
		case <-p.stalled:
			<-p.released
			return
		default:
		}
		if err != nil {
			return
		}
		if _, err = dst.Write(buf[:n]); err != nil {
			return
		}
	}
}

func throughProxy(t *testing.T, cfg *rabbitmq.Config) (*faultyProxy, *rabbitmq.Config) {
	t.Helper()

	proxy := newFaultyProxy(t, cfg.URL)
	proxied := *cfg
	proxied.URL = proxy.url(t, cfg.URL)
	return proxy, &proxied
}

func within(t *testing.T, limit time.Duration, name string, call func() error) error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		t.Fatalf("%s is still blocked after %s", name, limit)
		return nil
	}
}

func TestPublishIsBoundedWhenTheBrokerStopsResponding(t *testing.T) {
	brokerURL(t)

	payloads := map[string][]byte{
		"confirm never arrives": []byte("small"),
		// Larger than the socket buffers, so the write itself blocks.
		"write blocks": make([]byte, 32<<20),
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			cfg := newConfig(t)
			proxy, proxied := throughProxy(t, cfg)
			proxied.ConfirmTimeout = confirmTimeout
			pub := newPublisher(t, proxied)
			require.NoError(t, pub.Publish(topic, message.NewMessage("warm-up-uuid", []byte("warm-up"))))

			proxy.stall()
			started := time.Now()
			err := within(t, 10*confirmTimeout, "Publish", func() error {
				return pub.Publish(topic, message.NewMessage("lost-uuid", payload))
			})
			require.Error(t, err)
			assert.Less(t, time.Since(started), 2*confirmTimeout)

			started = time.Now()
			require.NoError(t, within(t, 10*confirmTimeout, "Close", pub.Close))
			assert.Less(t, time.Since(started), confirmTimeout)
		})
	}
}

func TestPublishUnroutable(t *testing.T) {
	cfg := newConfig(t)
	pub := newPublisher(t, cfg)
	require.NoError(t, pub.Publish(topic, message.NewMessage("uuid-1", []byte("payload-1"))))

	_, err := rawChannel(t, cfg.URL).QueueDelete(cfg.Queue, false, false, false)
	require.NoError(t, err)

	err = pub.Publish(topic, message.NewMessage("unroutable-uuid", []byte("payload-2")))
	require.ErrorIs(t, err, rabbitmq.ErrUnroutable)

	require.NoError(t, pub.Publish(topic, message.NewMessage("uuid-3", []byte("payload-3"))))
	got := receive(t, subscribe(t, newSubscriber(t, cfg)))
	assert.Equal(t, "uuid-3", got.UUID)
	got.Ack()
}

func publishOnceReconnected(t *testing.T, pub *rabbitmq.Publisher, msg *message.Message) {
	t.Helper()
	require.Eventually(t, func() bool { return pub.Publish(topic, msg) == nil }, receiveTimeout, 50*time.Millisecond)
}

func TestPublisherRecoversAfterItsConnectionDrops(t *testing.T) {
	cfg := newConfig(t)
	proxy, proxied := throughProxy(t, cfg)
	pub := newPublisher(t, proxied)
	require.NoError(t, pub.Publish(topic, message.NewMessage("before-uuid", []byte("before"))))

	proxy.sever()
	publishOnceReconnected(t, pub, message.NewMessage("after-uuid", []byte("after")))

	messages := subscribe(t, newSubscriber(t, cfg))
	for _, want := range []string{"before-uuid", "after-uuid"} {
		got := receive(t, messages)
		assert.Equal(t, want, got.UUID)
		got.Ack()
	}
}

func TestPublisherRecoversAfterItsExchangeIsDeleted(t *testing.T) {
	cfg := newConfig(t)
	pub := newPublisher(t, cfg)
	require.NoError(t, pub.Publish(topic, message.NewMessage("uuid-1", []byte("payload-1"))))

	require.NoError(t, rawChannel(t, cfg.URL).ExchangeDelete(cfg.Exchange, false, false))

	err := pub.Publish(topic, message.NewMessage("lost-uuid", []byte("payload-2")))
	require.ErrorIs(t, err, rabbitmq.ErrNoConfirm)
	assert.Contains(t, err.Error(), "NOT_FOUND")

	publishOnceReconnected(t, pub, message.NewMessage("uuid-3", []byte("payload-3")))
	messages := subscribe(t, newSubscriber(t, cfg))
	for _, want := range []string{"uuid-1", "uuid-3"} {
		got := receive(t, messages)
		assert.Equal(t, want, got.UUID)
		got.Ack()
	}
}

func TestSubscriberResumesAfterItsConnectionDrops(t *testing.T) {
	cfg := newConfig(t)
	proxy, proxied := throughProxy(t, cfg)
	messages := subscribe(t, newSubscriber(t, proxied))
	pub := newPublisher(t, cfg)
	require.NoError(t, pub.Publish(topic, message.NewMessage("held-uuid", []byte("held"))))
	held := receive(t, messages)

	proxy.sever()

	again := receive(t, messages)
	assert.Equal(t, "held-uuid", again.UUID)
	assert.True(t, redelivered(t, again), "the broker requeues what the dropped connection held unacked")
	held.Ack()
	again.Ack()

	require.NoError(t, pub.Publish(topic, message.NewMessage("next-uuid", []byte("next"))))
	next := receive(t, messages)
	assert.Equal(t, "next-uuid", next.UUID)
	next.Ack()
}

func TestSubscriberResumesAfterItsQueueIsDeleted(t *testing.T) {
	cfg := newConfig(t)
	messages := subscribe(t, newSubscriber(t, cfg))

	_, err := rawChannel(t, cfg.URL).QueueDelete(cfg.Queue, false, false, false)
	require.NoError(t, err)
	requireConsumer(t, cfg)

	require.NoError(t, newPublisher(t, cfg).Publish(topic, message.NewMessage("uuid-1", []byte("payload"))))
	got := receive(t, messages)
	assert.Equal(t, "uuid-1", got.UUID)
	got.Ack()
}

// requireConsumer waits for the subscriber to redeclare its queue and consume it again.
func requireConsumer(t *testing.T, cfg *rabbitmq.Config) {
	t.Helper()

	conn, err := amqp.Dial(cfg.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	require.Eventually(t, func() bool { return consumers(conn, cfg.Queue) == 1 }, receiveTimeout, 100*time.Millisecond)
}

// A passive declare of a missing queue closes its channel, hence one channel per look.
func consumers(conn *amqp.Connection, queue string) int {
	ch, err := conn.Channel()
	if err != nil {
		return 0
	}
	defer ch.Close()

	declared, err := ch.QueueDeclarePassive(queue, true, false, false, false, nil)
	if err != nil {
		return 0
	}
	return declared.Consumers
}

func TestPublisherClose(t *testing.T) {
	pub := newPublisher(t, newConfig(t))
	require.NoError(t, pub.Close())

	err := pub.Publish(topic, message.NewMessage("closed-uuid", []byte("payload")))

	assert.ErrorIs(t, err, rabbitmq.ErrPublisherClosed)
}

func TestSubscriberCloseStopsChannel(t *testing.T) {
	sub := newSubscriber(t, newConfig(t))
	messages := subscribe(t, sub)

	require.NoError(t, sub.Close())

	select {
	case _, ok := <-messages:
		assert.False(t, ok, "expected channel to be closed after subscriber Close")
	case <-time.After(receiveTimeout):
		t.Fatal("timed out waiting for message channel to close")
	}
}
