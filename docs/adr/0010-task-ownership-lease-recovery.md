# ADR-0010: Task ownership, leases, and generation fencing for crash-safe dispatch

- Status: Accepted (merged in #11: pocket is crash-safe. Microservice mode reaches this API over gRPC in [ADR-0011](0011-rabbitmq-dispatch.md). Lifecycle events are notifications only; the task row is written through the ownership API.)
- Date: 2026-11-16
- Deciders: project

## Context
With the transactional outbox ([ADR-0003](0003-transactional-outbox.md)) a dispatch event is published durably, but once a worker picks up a task nothing owns it. A worker that crashes mid-download leaves the task stuck with no owner and no retry; a lost ack or a slow worker lets two workers run the same task and race on the stored object. Delivery is at-least-once (consumers ack after completion, [ADR-0004](0004-consumer-ack-after-completion.md)), so duplicate dispatch is normal and must be made safe, not merely rare. Pocket mode has no broker redelivery at all, so recovery cannot depend on the message layer.

## Decision
Make the task row the single source of truth for who is running a task, and fence every write by a monotonic generation token.
- Ownership columns on the task row: `owner_id`, `lease_expires_at`, `attempt_generation`, `retry_count`, `next_attempt_at`.
- A fenced attempt API on the task service: `ClaimTask` (bumps `attempt_generation`, sets owner + lease), `RenewLease` (heartbeat), `CompleteAttempt` (idempotent for a lost response), `FailAttempt` (retry vs terminal), `ReleaseAttempt` (voluntary). Every call carries the generation of its claim; a call on a stale generation or an expired lease is rejected.
- Per-attempt storage keys (`{taskID}/{generation}/...`) so a stale attempt can only delete its own object, never the winner's.
- A periodic reconciler redispatches tasks that are PENDING-and-due or whose lease expired. It is the only recovery mechanism in pocket and the state-layer backstop in microservice mode.
- Completion writes a `TaskCompleted` outbox event in the same transaction ([ADR-0003](0003-transactional-outbox.md)).

## Consequences
- Exactly one attempt's result is authoritative, even under duplicate dispatch, a lost ack, or a crashed worker; the fence makes at-least-once delivery safe.
- Recovery is layered: the reconciler (state) and, in microservice mode, broker redelivery both drive redispatch, and they do not conflict because the generation fence dedupes - one claim wins, the rest ack-and-drop.
- Pocket becomes crash-safe without a broker.
- Cost: more columns, a reconciler loop, and a heartbeat. User-pause clears ownership and restarts the download from zero; byte-level resume is a later stage.
- The MySQL ownership queries are generated with sqlc, like the rest of the task store.

## Alternatives considered
- Broker-only recovery (RabbitMQ requeue / redelivery) as the sole mechanism: rejected because it does not cover pocket (no broker), and a broker tracks messages, not task state, so a redelivered message still needs a state check to avoid a double-run. Kept as a complementary layer in microservice mode (a separate ADR covers the RabbitMQ dispatch split).
- A distributed lock (Redis/etcd) per task: rejected; it adds a datastore and a second source of truth beside the task row, and still needs fencing to be correct because a lock holder can stall. The DB row plus a generation token is simpler and already transactional.
- Optimistic status-only updates without a generation: rejected; a bare status check is not a fence, so a stale worker could still overwrite the winner's result.
