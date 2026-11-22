package task

import (
	"context"
	"time"
)

// StatusCounter counts tasks by status for the tasks-by-status gauge. It is a
// capability separate from Repository, so the many Repository fakes need not
// implement it; the mysql and sqlite repositories do, and cmd type-asserts it.
type StatusCounter interface {
	CountByStatus(ctx context.Context) (map[TaskStatus]int64, error)
}

// Metrics records task-service domain metrics. The service, relay and
// reconciler call the push methods; the observable gauges (outbox backlog,
// tasks-by-status) are registered in cmd. nopMetrics is the default, so core
// needs no backend (CC-2).
type Metrics interface {
	// RecordDispatchWait records the delay from task creation to its first
	// committed claim (recorded once per task).
	RecordDispatchWait(d time.Duration)
	// IncCompleted, IncFailed, IncRetried count terminal attempt outcomes.
	IncCompleted()
	IncFailed()
	IncRetried()
	// RecordRelayPublish counts one outbox publish, by result.
	RecordRelayPublish(ok bool)
	// IncRedispatched counts one reconciler redispatch.
	IncRedispatched()
}

type nopMetrics struct{}

func (nopMetrics) RecordDispatchWait(time.Duration) {}
func (nopMetrics) IncCompleted()                    {}
func (nopMetrics) IncFailed()                       {}
func (nopMetrics) IncRetried()                      {}
func (nopMetrics) RecordRelayPublish(bool)          {}
func (nopMetrics) IncRedispatched()                 {}
