// Adapted from watermill-amqp v2.1.3, pkg/amqp/marshaler.go. License notice in doc.go.

package rabbitmq

import (
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/yuisofull/goload/pkg/message"
)

const maxShortStringBytes = 255

var ErrShortStringTooLong = errors.New("rabbitmq: value exceeds the 255 byte AMQP short string limit")

type Marshaler interface {
	Marshal(msg *message.Message) (amqp.Publishing, error)
}

type Unmarshaler interface {
	Unmarshal(delivery amqp.Delivery) (*message.Message, error)
}

type MarshalerUnmarshaler interface {
	Marshaler
	Unmarshaler
}

type DefaultMarshaler struct{}

func (DefaultMarshaler) Marshal(msg *message.Message) (amqp.Publishing, error) {
	if len(msg.UUID) > maxShortStringBytes {
		return amqp.Publishing{}, fmt.Errorf("message UUID: %w", ErrShortStringTooLong)
	}

	headers := make(amqp.Table, len(msg.Metadata))
	for key, value := range msg.Metadata {
		if len(key) > maxShortStringBytes {
			return amqp.Publishing{}, fmt.Errorf("metadata key: %w", ErrShortStringTooLong)
		}
		headers[key] = value
	}

	return amqp.Publishing{
		MessageId:    msg.UUID,
		Headers:      headers,
		DeliveryMode: amqp.Persistent,
		Body:         msg.Payload,
	}, nil
}

func (DefaultMarshaler) Unmarshal(delivery amqp.Delivery) (*message.Message, error) {
	metadata := make(message.Metadata, len(delivery.Headers))
	for key, value := range delivery.Headers {
		if text, ok := headerText(value); ok {
			metadata.Set(key, text)
		}
	}

	msg := message.NewMessage(delivery.MessageId, delivery.Body)
	msg.Metadata = metadata

	return msg, nil
}

// The broker adds non-string headers (x-delivery-count, x-death); they must not fail a delivery.
func headerText(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	case nil, amqp.Table, []any:
		return "", false
	default:
		return fmt.Sprint(v), true
	}
}
