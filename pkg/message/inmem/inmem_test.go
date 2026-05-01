package inmem

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/yuisofull/goload/pkg/message"
)

func TestPublishSubscribe_Ack(t *testing.T) {
	pub, sub := NewPublisherAndSubscriber(log.NewNopLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := sub.Subscribe(ctx, "topic1")
	if err != nil {
		t.Fatal(err)
	}

	msg := message.NewMessage("1", []byte("hello"))

	if err := pub.Publish("topic1", msg); err != nil {
		t.Fatal(err)
	}

	select {
	case m := <-ch:
		if m == nil {
			t.Fatal("nil message")
		}
		assert.Equal(t, "1", m.UUID)
		assert.Equal(t, []byte("hello"), []byte(m.Payload))
		m.Ack()

	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestPublishSubscribe_NackRedeliver(t *testing.T) {
	pub, sub := NewPublisherAndSubscriber(log.NewNopLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := sub.Subscribe(ctx, "topic2")
	if err != nil {
		t.Fatal(err)
	}

	msg := message.NewMessage("2", []byte("world"))

	if err := pub.Publish("topic2", msg); err != nil {
		t.Fatal(err)
	}

	// receive first, Nack -> expect redelivery
	select {
	case m := <-ch:
		if m == nil {
			t.Fatal("nil message")
		}
		m.Nack()
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first message")
	}

	select {
	case m := <-ch:
		if m == nil {
			t.Fatal("nil message on redelivery")
		}
		m.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for redelivered message")
	}
}
