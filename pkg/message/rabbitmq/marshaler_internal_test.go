package rabbitmq

import (
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/pkg/message"
)

func deliveryOf(p amqp.Publishing) amqp.Delivery {
	return amqp.Delivery{
		MessageId:    p.MessageId,
		Headers:      p.Headers,
		DeliveryMode: p.DeliveryMode,
		Body:         p.Body,
	}
}

func TestDefaultMarshaler_RoundTrip(t *testing.T) {
	want := message.NewMessage("outbox-event-1", []byte(`{"task_id":42}`))
	want.Metadata.Set("eventType", "TaskCreated")
	want.Metadata.Set("taskID", "42")

	publishing, err := DefaultMarshaler{}.Marshal(want)
	require.NoError(t, err)

	got, err := DefaultMarshaler{}.Unmarshal(deliveryOf(publishing))
	require.NoError(t, err)

	assert.True(t, want.Equals(got), "want %+v, got %+v", want, got)
}

func TestDefaultMarshaler_Marshal_MapsMessageOntoPublishing(t *testing.T) {
	msg := message.NewMessage("uuid-1", []byte("payload"))
	msg.Metadata.Set("eventType", "TaskCreated")

	publishing, err := DefaultMarshaler{}.Marshal(msg)
	require.NoError(t, err)

	assert.Equal(t, "uuid-1", publishing.MessageId)
	assert.Equal(t, []byte("payload"), publishing.Body)
	assert.Equal(t, amqp.Table{"eventType": "TaskCreated"}, publishing.Headers)
	assert.Equal(t, amqp.Persistent, publishing.DeliveryMode)
	assert.NoError(t, publishing.Headers.Validate())
}

func TestDefaultMarshaler_Marshal_AcceptsMessageWithoutMetadata(t *testing.T) {
	msg := &message.Message{UUID: "uuid-1", Payload: []byte("payload")}

	publishing, err := DefaultMarshaler{}.Marshal(msg)
	require.NoError(t, err)

	assert.Equal(t, "uuid-1", publishing.MessageId)
	assert.Empty(t, publishing.Headers)
}

func TestDefaultMarshaler_Marshal_RejectsOversizedShortStrings(t *testing.T) {
	oversized := strings.Repeat("a", maxShortStringBytes+1)

	t.Run("uuid", func(t *testing.T) {
		_, err := DefaultMarshaler{}.Marshal(message.NewMessage(oversized, nil))
		assert.ErrorIs(t, err, ErrShortStringTooLong)
	})

	t.Run("metadata key", func(t *testing.T) {
		msg := message.NewMessage("uuid-1", nil)
		msg.Metadata.Set(oversized, "value")

		_, err := DefaultMarshaler{}.Marshal(msg)
		assert.ErrorIs(t, err, ErrShortStringTooLong)
	})

	t.Run("at the limit", func(t *testing.T) {
		atLimit := strings.Repeat("a", maxShortStringBytes)
		msg := message.NewMessage(atLimit, nil)
		msg.Metadata.Set(atLimit, "value")

		_, err := DefaultMarshaler{}.Marshal(msg)
		assert.NoError(t, err)
	})
}

func TestDefaultMarshaler_Unmarshal_ToleratesBrokerHeaders(t *testing.T) {
	delivery := amqp.Delivery{
		MessageId: "uuid-1",
		Body:      []byte("payload"),
		Headers: amqp.Table{
			"eventType":            "TaskCreated",
			"x-delivery-count":     int64(3),
			"x-first-death-reason": "rejected",
			"x-death":              []any{amqp.Table{"count": int64(1)}},
			"x-nested":             amqp.Table{"k": "v"},
			"raw":                  []byte("bytes"),
			"flag":                 true,
			"nothing":              nil,
		},
	}

	msg, err := DefaultMarshaler{}.Unmarshal(delivery)
	require.NoError(t, err)

	assert.Equal(t, "uuid-1", msg.UUID)
	assert.Equal(t, message.Payload("payload"), msg.Payload)
	assert.Equal(t, message.Metadata{
		"eventType":            "TaskCreated",
		"x-delivery-count":     "3",
		"x-first-death-reason": "rejected",
		"raw":                  "bytes",
		"flag":                 "true",
	}, msg.Metadata)
}

func TestDefaultMarshaler_Unmarshal_ReturnsSettleableMessage(t *testing.T) {
	msg, err := DefaultMarshaler{}.Unmarshal(amqp.Delivery{MessageId: "uuid-1"})
	require.NoError(t, err)

	require.True(t, msg.Ack())
	select {
	case <-msg.Acked():
	default:
		t.Fatal("Acked channel is not closed after Ack")
	}
}
