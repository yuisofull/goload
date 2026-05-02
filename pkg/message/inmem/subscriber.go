package inmem

import (
	"context"
	"sync"

	"github.com/go-kit/log"

	"github.com/yuisofull/goload/pkg/message"
)

type subscription struct {
	ch     chan *message.Message
	ctx    context.Context
	cancle context.CancelFunc
}

type broker struct {
	mu     sync.RWMutex
	buffer int
	subs   map[string][]*subscription
	logger log.Logger
}

func newBroker(buffer int, logger log.Logger) *broker {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &broker{buffer: buffer, subs: make(map[string][]*subscription), logger: logger}
}

func (b *broker) Close() error {
	// best-effort: drop all subs
	b.mu.Lock()
	defer b.mu.Unlock()
	for topic, arr := range b.subs {
		for _, sub := range arr {
			close(sub.ch)
		}
		delete(b.subs, topic)
	}
	return nil
}

func (b *broker) addSub(topic string, s *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[topic] = append(b.subs[topic], s)
}

func (b *broker) removeSub(topic string, s *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	arr := b.subs[topic]
	for i := range arr {
		if arr[i] == s {
			b.subs[topic] = append(arr[:i], arr[i+1:]...)
			break
		}
	}
	if len(b.subs[topic]) == 0 {
		delete(b.subs, topic)
	}
}

func (b *broker) getSubs(topic string) []*subscription {
	b.mu.RLock()
	defer b.mu.RUnlock()
	arr := b.subs[topic]
	// return a copy to avoid races
	out := make([]*subscription, len(arr))
	copy(out, arr)
	return out
}

type Subscriber struct {
	b *broker
}

func NewSubscriber(b *broker) *Subscriber {
	return &Subscriber{b: b}
}

func (s *Subscriber) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	ch := make(chan *message.Message, s.b.buffer)
	ctx, cancle := context.WithCancel(ctx)
	sub := &subscription{ch: ch, ctx: ctx, cancle: cancle}
	s.b.addSub(topic, sub)

	// remove subscription when ctx done
	go func() {
		<-ctx.Done()
		s.b.removeSub(topic, sub)
		close(ch)
	}()

	return ch, nil
}

func (s *Subscriber) Close() error {
	// best-effort: drop all subs
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	for topic, arr := range s.b.subs {
		for _, sub := range arr {
			close(sub.ch)
			sub.cancle()
		}
		delete(s.b.subs, topic)
	}
	return nil
}

// helpers to create shared broker + pub/sub
func NewBroker(buffer int, logger log.Logger) *broker { return newBroker(buffer, logger) }

func NewPublisherAndSubscriber(logger log.Logger) (*Publisher, *Subscriber) {
	b := newBroker(100, logger) // default buffer size
	return NewPublisher(b), NewSubscriber(b)
}
