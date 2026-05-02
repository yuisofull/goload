package inmem

import (
	"context"

	"github.com/go-kit/log/level"

	"github.com/yuisofull/goload/pkg/message"
)

type Publisher struct {
	broker *broker
}

func NewPublisher(b *broker) *Publisher {
	return &Publisher{broker: b}
}

func (p *Publisher) Publish(topic string, msgs ...*message.Message) error {
	subs := p.broker.getSubs(topic)
	_ = level.Debug(p.broker.logger).
		Log("msg", "inmem.publish", "topic", topic, "messages", len(msgs), "subscribers", len(subs))
	for mi, msg := range msgs {
		for si, s := range subs {
			// deliver a copy per subscriber
			m := msg.Copy()
			m.SetContext(s.ctx)

			// non-blocking send with goroutine to avoid blocking publisher
			go func(ch chan *message.Message, mm *message.Message, subCtx context.Context, topic string, mi, si int) {
				level.Debug(p.broker.logger).
					Log("msg", "inmem.sending", "topic", topic, "msg_index", mi, "sub_index", si)
				select {
				case ch <- mm:
					level.Debug(p.broker.logger).Log("msg", "inmem.sent", "msg_index", mi, "sub_index", si)
				case <-subCtx.Done():
					level.Debug(p.broker.logger).
						Log("msg", "inmem.subscriber_done_before_send", "msg_index", mi, "sub_index", si)
					return
				}

				// wait for ack/nack and handle redelivery on Nack
				for {
					select {
					case <-mm.Acked():
						level.Debug(p.broker.logger).Log("msg", "inmem.acked", "msg_index", mi, "sub_index", si)
						return
					case <-mm.Nacked():
						level.Warn(p.broker.logger).Log("msg", "inmem.nacked", "msg_index", mi, "sub_index", si)
						// create a fresh copy and resend
						mm = mm.Copy()
						mm.SetContext(subCtx)
						select {
						case ch <- mm:
							level.Debug(p.broker.logger).
								Log("msg", "inmem.redelivered", "msg_index", mi, "sub_index", si)
						case <-subCtx.Done():
							level.Debug(p.broker.logger).
								Log("msg", "inmem.subscriber_done_during_redeliver", "msg_index", mi, "sub_index", si)
							return
						}
					case <-subCtx.Done():
						_ = level.Debug(p.broker.logger).
							Log("msg", "inmem.subscriber_done_waiting_ack", "msg_index", mi, "sub_index", si)
						return
					}
				}
			}(s.ch, m, s.ctx, topic, mi, si)
		}
	}
	return nil
}

func (p *Publisher) Close() error { return nil }
