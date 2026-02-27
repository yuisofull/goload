# pkg/message

The `pkg/message` package provides a **minimal, Watermill-inspired** pub/sub abstraction for inter-service messaging. It defines the core `Message` type and `Publisher`/`Subscriber` interfaces, along with a Kafka implementation backed by [IBM/sarama](https://github.com/IBM/sarama).

> Code is adapted from [Watermill](https://github.com/ThreeDotsLabs/watermill) (Apache 2.0).

---

## Package structure

```
pkg/message/
├── message.go    ← Message struct + Ack/Nack mechanics
├── metadata.go   ← Metadata type (map[string]string)
├── pubsub.go     ← Publisher and Subscriber interfaces
└── kafka/
    ├── publisher.go    ← Kafka publisher (sarama SyncProducer)
    ├── subscriber.go   ← Kafka subscriber (sarama ConsumerGroup)
    ├── marshaler.go    ← Message ↔ Kafka record marshaling
    └── context.go      ← Kafka-specific context helpers
```

---

## Core types

### `Message`

```go
type Message struct {
    UUID     string
    Metadata Metadata   // map[string]string – like HTTP headers
    Payload  Payload    // []byte – the raw message body
    // internal ack/nack channels (unexported)
}
```

A `Message` carries a unique UUID, key-value metadata, and a binary payload. After a subscriber receives a message it **must** call either `Ack()` or `Nack()`.

| Method | Description |
|--------|-------------|
| `Ack() bool` | Acknowledge: message processed successfully. Returns `false` if `Nack` was already sent. |
| `Nack() bool` | Negative-acknowledge: message should be redelivered. Returns `false` if `Ack` was already sent. |
| `Acked() <-chan struct{}` | Channel closed when `Ack` is received |
| `Nacked() <-chan struct{}` | Channel closed when `Nack` is received |
| `Context() context.Context` | Returns the message's context (defaults to `context.Background()`) |
| `SetContext(ctx)` | Attach a context to the message |
| `Copy() *Message` | Deep-copy without propagating ack state or context |
| `Equals(other) bool` | Compare UUID, metadata, and payload |

**Constructor:**
```go
msg := message.NewMessage(uuid, payload)
```

### `Metadata`

```go
type Metadata map[string]string
```

| Method | Description |
|--------|-------------|
| `Get(key) string` | Returns value for key, or `""` if missing |
| `Set(key, value)` | Sets a key-value pair |

---

## Interfaces

### `Publisher`

```go
type Publisher interface {
    Publish(topic string, messages ...*Message) error
    Close() error
}
```

- `Publish` sends one or more messages to a topic. **Thread-safe.**
- `Close` flushes any buffered messages and releases resources.

### `Subscriber`

```go
type Subscriber interface {
    Subscribe(ctx context.Context, topic string) (<-chan *Message, error)
    Close() error
}
```

- `Subscribe` returns a channel that delivers messages from the topic.
- The channel is closed when the context is cancelled or `Close()` is called.
- **Each message must be `Ack()`ed to receive the next one** (back-pressure).
- `Close` cancels all active subscriptions.

### `SubscribeInitializer` (optional)

```go
type SubscribeInitializer interface {
    SubscribeInitialize(topic string) error
}
```

Some backends (e.g. Kafka) require topics to be created before publishing. Implement this if the backend needs explicit initialization.

---

## Kafka implementation (`pkg/message/kafka`)

### Publisher

```go
pub, err := kafka.NewPublisher(&kafka.PublisherConfig{
    BrokerHosts: []string{"broker:9092"},
    Version:     sarama.V4_0_0_0,
    ClientID:    "my-service",
    MaxRetry:    3,
    Marshaler:   kafka.DefaultMarshaler{}, // optional, default used if nil
})
```

Uses a **sarama `SyncProducer`** — each `Publish` call blocks until the broker acknowledges (`WaitForAll` acks).

### Subscriber

```go
sub, err := kafka.NewSubscriber(&kafka.SubscriberConfig{
    Brokers:             []string{"broker:9092"},
    ConsumerGroup:       "my-service-group",  // required
    Version:             sarama.V4_0_0_0,
    AutoCommit:          false,
    NackResendSleep:     100 * time.Millisecond,
    ReconnectRetrySleep: time.Second,
    Unmarshaler:         kafka.DefaultMarshaler{}, // optional
}, kafka.WithErrorHandler(func(ctx context.Context, err error) {
    log.Println("kafka error:", err)
}))
```

- Uses a **sarama `ConsumerGroup`** internally.
- `NackResendSleep` — how long to wait before redelivering a Nacked message.
- `ReconnectRetrySleep` — delay between reconnect attempts on broker failure.
- A consumer group name is **required**.
- Offsets start from `OffsetOldest` by default.

### Marshaler / Unmarshaler

`DefaultMarshaler` maps `Message` ↔ `sarama.ProducerMessage` / `sarama.ConsumerMessage`:

| Direction | Key | Value | Headers |
|-----------|-----|-------|---------|
| Marshal | _(none)_ | `Payload` bytes | `_watermill_message_uuid` + all `Metadata` entries |
| Unmarshal | — | `Payload` bytes | UUID from `_watermill_message_uuid` header, rest → `Metadata` |

> The header key `_watermill_message_uuid` is reserved. Do not put it in `Metadata` manually.

**Partition-key marshaler** (for ordered delivery):

```go
marshaler := kafka.NewWithPartitioningMarshaler(func(topic string, msg *message.Message) (string, error) {
    return msg.Metadata.Get("taskID"), nil
})
```

### Context helpers (`kafka/context.go`)

When consuming messages, the subscriber injects Kafka-specific values into the message context:

| Function | Context key | Type | Description |
|----------|------------|------|-------------|
| `MessagePartitionFromCtx(ctx)` | `partitionContextKey` | `int32` | Kafka partition the message came from |
| `MessagePartitionOffsetFromCtx(ctx)` | `partitionOffsetContextKey` | `int64` | Kafka offset within the partition |
| `MessageTimestampFromCtx(ctx)` | `timestampContextKey` | `time.Time` | Kafka message timestamp |
| `MessageKeyFromCtx(ctx)` | `keyContextKey` | `[]byte` | Kafka message key |

---

## Usage pattern in this project

### Publishing (Task Service / Download Service)

```go
pub, _ := kafkapkg.NewPublisher(pubCfg)

msg := &message.Message{
    UUID:    uuid.New().String(),
    Payload: jsonBytes,
    Metadata: message.Metadata{
        "eventType": "TaskCreated",
    },
}
pub.Publish("task.created", msg)
```

### Consuming (Download Service / Task Service event consumer)

```go
sub, _ := kafkapkg.NewSubscriber(subCfg)

ch, _ := sub.Subscribe(ctx, "task.created")
for msg := range ch {
    if err := handle(msg); err != nil {
        msg.Nack() // redelivery
    } else {
        msg.Ack()  // commit offset
    }
}
```

---

## Design notes

- The package intentionally mirrors [Watermill](https://github.com/ThreeDotsLabs/watermill)'s message model to make it easy to swap in the full Watermill library later.
- Ack/Nack channels are used instead of callbacks to allow `select`-based flow control.
- The `Subscriber` channel model naturally provides back-pressure: the next message is only delivered after the previous one is acked.
- Adding a new backend (e.g. RabbitMQ, NATS) only requires implementing the `Publisher` and `Subscriber` interfaces.

---

## Testing

The Kafka implementation is covered by integration tests in `pkg/message/kafka/kafka_test.go` using [testcontainers-go](https://github.com/testcontainers/testcontainers-go) to spin up a real Kafka broker in Docker.

### Test cases

| Test | What it verifies |
|------|-----------------|
| `TestPublishSubscribe` | A published message arrives at the subscriber with the correct UUID, payload, and metadata. |
| `TestPublishMultipleMessages` | Multiple messages are delivered in order on a single-partition topic. |
| `TestNackRedelivery` | Nacking a message causes it to be re-delivered before the subscriber moves on. |
| `TestPublisherClose` | Calling `Close()` on the publisher returns an error on subsequent `Publish` calls. |
| `TestSubscriberCloseStopsChannel` | Calling `Close()` on the subscriber closes the output channel. |
| `TestSubscriberRequiresConsumerGroup` | `NewSubscriber` returns `ErrConsumerGroupEmpty` when no consumer group is set. |
| `TestMessageMetadataRoundtrip` | All metadata key-value pairs survive the Marshal → Kafka → Unmarshal round-trip. |
| `TestContextValuesInjected` | The subscriber injects Kafka partition, offset, and timestamp into the message context. |

### Running the tests

Requires Docker (e.g. Docker Desktop with WSL 2 integration enabled):

```bash
# Run all Kafka integration tests
go test ./pkg/message/kafka/... -v -timeout 120s

# Skip integration tests (unit-only)
go test ./pkg/message/kafka/... -short
```

Structured logs from the publisher and subscriber are emitted to `stderr` during tests via `go-kit/log` in logfmt format.


