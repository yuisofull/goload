package task_v2

import (
	"context"
	"encoding/json"
	"github.com/yuisofull/goload/pkg/message"
)

const (
	createdTopic = "download_task.created"
)

type createdProducer struct {
	publisher message.Publisher
}

func NewDownloadTaskCreatedProducer(publisher message.Publisher) CreatedProducer {
	return &createdProducer{publisher: publisher}
}

func (p *createdProducer) Produce(ctx context.Context, event CreatedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	msg := message.NewMessage("", payload)
	return p.publisher.Publish(createdTopic, msg)
}
