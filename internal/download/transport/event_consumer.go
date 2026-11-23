package downloadtransport

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/yuisofull/goload/internal/download"
	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/pkg/message"
	"github.com/yuisofull/goload/pkg/message/rabbitmq"
)

const (
	// DefaultRedeliveryDelay is how long a dispatch whose attempt could not be
	// settled is held before it is negatively acknowledged. The in-memory bus
	// redelivers at once, so without the pause a task service that keeps
	// failing would turn redelivery into a busy loop.
	DefaultRedeliveryDelay = time.Second

	// attemptDrainTimeout bounds how long Start waits, after its context is
	// cancelled, for running attempts to record their outcome.
	attemptDrainTimeout = 15 * time.Second
)

// EventConsumer handles incoming events for the download service.
type EventConsumer struct {
	service    download.Service
	subscriber message.Subscriber
	logger     log.Logger

	// attempts, when set, executes every task.created delivery as a claimed
	// attempt and acknowledges it only once the outcome is settled. When nil
	// the legacy path runs: ExecuteTask, acknowledged when the download starts.
	attempts        download.AttemptRunner
	redeliveryDelay time.Duration
	metrics         download.Metrics
	tracing         download.Tracing

	// dispatchSubscriber, when set, is where task.created is consumed (RabbitMQ);
	// control events stay on subscriber. Nil means all topics use subscriber.
	dispatchSubscriber message.Subscriber
	controlDisabled    bool
}

// Option configures an EventConsumer.
type Option func(*EventConsumer)

// WithAttemptRunner switches task.created deliveries to the ownership path:
// each one is executed with runner.RunAttempt (claim, heartbeat, commit) in
// its own goroutine (after taking a download slot when the runner is a
// download.AdmittingRunner), and the delivery is acknowledged only after RunAttempt
// returned nil.
func WithAttemptRunner(runner download.AttemptRunner) Option {
	return func(ec *EventConsumer) { ec.attempts = runner }
}

// WithRedeliveryDelay overrides DefaultRedeliveryDelay. Values that are zero
// or negative are ignored.
func WithRedeliveryDelay(d time.Duration) Option {
	return func(ec *EventConsumer) {
		if d > 0 {
			ec.redeliveryDelay = d
		}
	}
}

// WithDispatchSubscriber consumes task.created from a separate subscriber
// (RabbitMQ serves one topic per instance); control events stay on the main one.
func WithDispatchSubscriber(sub message.Subscriber) Option {
	return func(ec *EventConsumer) {
		if sub != nil {
			ec.dispatchSubscriber = sub
		}
	}
}

// WithMetrics injects a metrics recorder (used to count broker redeliveries).
func WithMetrics(m download.Metrics) Option {
	return func(ec *EventConsumer) {
		if m != nil {
			ec.metrics = m
		}
	}
}

// WithTracing continues each dispatch's trace from its message headers.
func WithTracing(t download.Tracing) Option {
	return func(ec *EventConsumer) {
		if t != nil {
			ec.tracing = t
		}
	}
}

// WithoutControlEvents skips the pause/cancel subscriptions. On the ownership
// path a stale, generation-blind control event could cancel a newer attempt.
func WithoutControlEvents() Option {
	return func(ec *EventConsumer) { ec.controlDisabled = true }
}

// NewEventConsumer creates a new event consumer for the download service.
func NewEventConsumer(
	service download.Service,
	subscriber message.Subscriber,
	logger log.Logger,
	opts ...Option,
) *EventConsumer {
	ec := &EventConsumer{
		service:         service,
		subscriber:      subscriber,
		logger:          logger,
		redeliveryDelay: DefaultRedeliveryDelay,
	}
	for _, o := range opts {
		o(ec)
	}
	return ec
}

// Start begins consuming events.
func (ec *EventConsumer) Start(ctx context.Context) error {
	dispatchSub := ec.subscriber
	if ec.dispatchSubscriber != nil {
		dispatchSub = ec.dispatchSubscriber
	}
	taskCreatedCh, err := dispatchSub.Subscribe(ctx, string(events.EventTaskCreated))
	if err != nil {
		return err
	}
	level.Info(ec.logger).Log("msg", "subscription started", "topic", "task.created")

	createdDone := make(chan struct{})
	go func() {
		defer close(createdDone)
		ec.processTaskCreatedEvents(ctx, taskCreatedCh)
	}()

	if !ec.controlDisabled {
		taskPausedCh, err := ec.subscriber.Subscribe(ctx, string(events.EventTaskPaused))
		if err != nil {
			return err
		}
		level.Info(ec.logger).Log("msg", "subscription started", "topic", "task.paused")

		taskCancelledCh, err := ec.subscriber.Subscribe(ctx, string(events.EventTaskCancelled))
		if err != nil {
			return err
		}
		level.Info(ec.logger).Log("msg", "subscription started", "topic", "task.cancelled")

		go ec.processTaskPausedEvents(ctx, taskPausedCh)
		go ec.processTaskCancelledEvents(ctx, taskCancelledCh)
	}

	level.Info(ec.logger).Log("msg", "event consumer running, awaiting context done")
	<-ctx.Done()
	level.Info(ec.logger).Log("msg", "event consumer stopping: context canceled")
	if ec.attempts != nil {
		// Running attempts stop with ctx and then record their outcome (a
		// release). Give them a bounded time, so the process does not close
		// the database underneath them.
		select {
		case <-createdDone:
		case <-time.After(attemptDrainTimeout):
			level.Warn(ec.logger).Log("msg", "attempts still running after the drain timeout")
		}
	}
	return ctx.Err()
}

func (ec *EventConsumer) processTaskCreatedEvents(ctx context.Context, ch <-chan *message.Message) {
	// Attempts run concurrently; the loop returns only after all of them
	// have settled.
	var running sync.WaitGroup
	defer running.Wait()

	for msg := range ch {
		if ec.metrics != nil {
			// The broker sets this per delivery on the dispatch (RabbitMQ) path;
			// it is absent (and a no-op) on the legacy Kafka path.
			if redelivered, ok := rabbitmq.MessageRedeliveredFromCtx(msg.Context()); ok && redelivered {
				ec.metrics.IncRedelivered()
			}
		}

		var event events.TaskCreatedEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			level.Error(ec.logger).Log("msg", "failed to unmarshal TaskCreatedEvent", "err", err)
			msg.Nack()
			continue
		}

		req := taskRequestFromCreatedEvent(event)
		level.Debug(ec.logger).Log(
			"msg", "executing task",
			"task_id", req.TaskID,
			"source_type", req.SourceType,
			"source_url_len", len(req.SourceURL),
		)

		if ec.attempts != nil {
			run := ec.attempts.RunAttempt
			if adm, ok := ec.attempts.(download.AdmittingRunner); ok {
				// Take the download slot BEFORE spawning the worker: the
				// goroutines in flight are bounded by the slots, and a
				// backlog waits in the subscription instead of piling up
				// as goroutines.
				if err := adm.Admit(ctx); err != nil {
					// Only a cancelled ctx stops admission: hand the
					// delivery back.
					msg.Nack()
					continue
				}
				run = adm.RunAdmittedAttempt
			}
			running.Add(1)
			go func() {
				defer running.Done()
				ec.runAttempt(ctx, msg, req, run)
			}()
			continue
		}

		if err := ec.service.ExecuteTask(ctx, req); err != nil {
			level.Error(ec.logger).Log("msg", "failed to execute task", "task_id", event.TaskID, "err", err)
			if isPermanentDownloadError(err) {
				msg.Ack()
				continue
			}
			msg.Nack()
			continue
		}
		// ExecuteTask returns after starting the download; completion is reported by task events.
		msg.Ack()
	}
}

// runAttempt executes one dispatch as a claimed attempt and acknowledges the
// delivery only once its outcome is settled: the result or the failure is
// committed by the task service, or there was nothing to run (see
// download.AttemptRunner).
func (ec *EventConsumer) runAttempt(
	ctx context.Context,
	msg *message.Message,
	req download.TaskRequest,
	run func(context.Context, download.TaskRequest) error,
) {
	// The span decorates the consumer's ctx (never replaces it), so a consumer
	// shutdown still cancels the attempt.
	runCtx, endSpan := ctx, func(error) {}
	if ec.tracing != nil {
		runCtx, endSpan = ec.tracing.StartConsume(ctx, msg.Metadata, req.TaskID)
	}
	err := run(runCtx, req)
	endSpan(err)
	if err == nil {
		msg.Ack()
		return
	}

	level.Error(ec.logger).Log(
		"msg", "attempt not settled, delivery will be redelivered",
		"task_id", req.TaskID,
		"err", err,
	)
	select {
	case <-time.After(ec.redeliveryDelay):
	case <-ctx.Done():
	}
	msg.Nack()
}

func (ec *EventConsumer) processTaskPausedEvents(ctx context.Context, ch <-chan *message.Message) {
	ec.processControlEvents(ch, "TaskPausedEvent", func(taskID uint64) error {
		return ec.service.PauseTask(ctx, taskID)
	})
}

func (ec *EventConsumer) processTaskCancelledEvents(ctx context.Context, ch <-chan *message.Message) {
	ec.processControlEvents(ch, "TaskCancelledEvent", func(taskID uint64) error {
		return ec.service.CancelTask(ctx, taskID)
	})
}

func (ec *EventConsumer) processControlEvents(
	ch <-chan *message.Message,
	eventName string,
	handle func(taskID uint64) error,
) {
	for msg := range ch {
		var event struct {
			TaskID uint64 `json:"task_id"`
		}
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			level.Error(ec.logger).Log("msg", "failed to unmarshal "+eventName, "err", err)
			msg.Nack()
			continue
		}

		if err := handle(event.TaskID); err != nil {
			level.Error(ec.logger).Log("msg", "failed to handle "+eventName, "task_id", event.TaskID, "err", err)
			if errors.IsError(err, errors.ErrCodeNotFound) {
				msg.Ack()
				continue
			}
			msg.Nack()
			continue
		}
		msg.Ack()
	}
}

func taskRequestFromCreatedEvent(event events.TaskCreatedEvent) download.TaskRequest {
	var req download.TaskRequest
	if event.SourceAuth != nil {
		req.SourceAuth = &download.AuthConfig{
			Type:     event.SourceAuth.Type,
			Username: event.SourceAuth.Username,
			Password: event.SourceAuth.Password,
			Token:    event.SourceAuth.Token,
		}
	}
	if event.DownloadOptions != nil {
		req.DownloadOptions = &download.DownloadOptions{
			Concurrency: event.DownloadOptions.Concurrency,
			MaxRetries:  event.DownloadOptions.MaxRetries,
		}
	}
	req.TaskID = event.TaskID
	req.OfAccountID = event.OfAccountID
	req.FileName = event.FileName
	req.SourceURL = event.SourceURL
	req.SourceType = event.SourceType
	req.Metadata = event.Metadata
	req.Checksum = nil
	req.CreatedAt = event.CreatedAt
	return req
}

func isPermanentDownloadError(err error) bool {
	return errors.IsError(err, errors.ErrCodeInvalidInput) || errors.IsError(err, errors.ErrCodeConflict)
}
