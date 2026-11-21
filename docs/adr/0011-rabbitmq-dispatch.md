# ADR-0011: RabbitMQ dispatch, Kafka lifecycle, and the ownership API over gRPC

- Status: Proposed
- Date: 2026-11-20
- Deciders: project

## Context
[ADR-0010](0010-task-ownership-lease-recovery.md) made pocket crash-safe, but microservice mode was still on the legacy path: `cmd/download` acked `task.created` when the download started and `cmd/task` applied lifecycle events unfenced (`WithLegacyStateWrites`), because a standalone worker could not reach the ownership API. Dispatch and lifecycle also have different shapes. Dispatch is a work queue: per-message ack, redelivery, dead-lettering, prefetch backpressure. Lifecycle (status, progress, completed, failed) is a replayable log read by several consumers. Kafka fits the second, not the first: the `pkg/message` Kafka subscriber holds one unacked message per partition, so acking a dispatch only once the attempt settles ([ADR-0004](0004-consumer-ack-after-completion.md)) means one running download per partition. The outbox already records a `destination` (`dispatch` or `lifecycle`) per event ([ADR-0003](0003-transactional-outbox.md)).

## Decision
Microservice mode reaches the ownership API over gRPC, dispatches on RabbitMQ, and keeps Kafka for lifecycle. Pocket is unchanged (in-process calls, in-memory bus). The switch is config-gated: with `TASK_SERVICE_GRPC_ADDRESS` and `RABBITMQ_URL` set the new path runs, otherwise the legacy Kafka-dispatch path is unchanged, so this is a non-breaking rollout.

- Ownership over gRPC as its OWN service. Per [API-16](../standards/api-design.md) (worker-only RPCs get their own service), the five RPCs (`ClaimTask`, `RenewLease`, `CompleteAttempt`, `FailAttempt`, `ReleaseAttempt`) live in a new `TaskOwnership` service, not on `TaskService`. `cmd/download` calls them through a client that satisfies the same `AttemptCoordinator` port pocket uses in-process; the contract types live in `internal/events` so neither service imports the other ([CC-3](../standards/clean-code.md)). Responses carry the granted lease duration, never a server timestamp; the client turns it into a local monotonic deadline. The reconciler runs inside `cmd/task` and has no RPC.
- Lifecycle events are notifications only: `WithLegacyStateWrites` is removed, so nothing on the bus writes the task row.
- RabbitMQ is a third `pkg/message` adapter beside `kafka` and `inmem`, adapted from watermill-amqp (copied in and self-maintained, as `pkg/message` itself is adapted from Watermill; not a go.mod dependency); core never imports amqp ([CC-2](../standards/clean-code.md)). The outbox relay routes by `destination`: `dispatch` to RabbitMQ, `lifecycle` to Kafka (`WithDispatchTransport`).
- Topology, declared idempotently by the adapter: durable direct exchange `goload.dispatch`, quorum queue `task.dispatch` with an explicit `x-delivery-limit` (default 5), a dead-letter exchange and queue `task.dispatch.dlq`. Publisher confirms plus the mandatory flag (an unroutable publish is an error, not a silent drop), writes bounded by a socket deadline (a blackholed broker fails the publish within the confirm timeout instead of wedging the connection), manual ack, prefetch equal to the worker's download slots, and a `consumer_timeout` (set in compose to 60m) above the 30m task timeout.
- Dispatch ack policy: ack once the attempt is settled through the ownership API; ack and drop a delivery that cannot be claimed (a redundant dispatch). A delivery that fails transiently before it settles is nacked with requeue, so the quorum queue redelivers it up to `x-delivery-limit` times and dead-letters it only after that, bounding retry without hot-looping a poison message; a message that cannot even be parsed is rejected straight to the dead-letter queue. The reconciler is the backstop for any task whose dispatch is ultimately dead-lettered or lost.

## Consequences
- Microservice mode gets the pocket guarantee: exactly one authoritative result. Duplicate execution can still happen briefly under failure; the fence and the per-attempt storage keys protect the committed result.
- Recovery is layered. The quorum queue redelivers a transiently-failed dispatch up to the delivery limit, then dead-letters it; the reconciler inside `cmd/task` re-dispatches any task that is due or whose lease expired, including one whose dispatch was dead-lettered or lost; and the fence dedupes when both fire. So recovery latency for a dead-lettered dispatch is the reconcile interval plus any lease wait, not an immediate redelivery.
- The dead-letter queue is a parking lot, not self-healing: it needs operational draining/monitoring. Two sharp edges with `x-delivery-limit`: (a) deliveries that a worker merely held unacked while shutting down (a crash, a dropped channel, a `consumer_timeout`) each spend one of the limit, so a dispatch unlucky across enough restarts dead-letters even without an explicit Nack - the reconciler still recovers it; (b) the queue argument is fixed at first declare, so both services must configure the same `DeliveryLimit`, or the declare fails with `PRECONDITION_FAILED` until the queue is deleted.
- Download concurrency follows prefetch instead of partition count.
- Cost: a second broker to run, and the first synchronous hop from download to task (deadlines per [MS-12](../standards/microservices.md)).
- Amends [ADR-0002](0002-async-download-pipeline.md): dispatch leaves Kafka. MS-6, MS-11 and ERR-15 are written for Kafka and should be read as covering the RabbitMQ ack and dead-letter rules too.

## Security considerations (follow-ups, not resolved here)
The `TaskOwnership` RPCs trust `worker_id` and the result's `storage_type` / `storage_key` from the caller, with no per-call authentication. This matches the existing posture of the whole task gRPC surface (internal, network-isolated, no per-call auth), so it is not a regression, but two hardenings are worth a separate decision with the maintainer:
- Worker authentication: mTLS client certs or a shared worker token compared with `subtle.ConstantTimeCompare`, with `workerID` derived from the verified identity rather than the request. This is a new auth mechanism and belongs in its own ADR.
- Storage-key validation: the server could reject a `CompleteAttempt` whose `storage_key` is not the generation-scoped prefix it expects (`{taskID}/{generation}/...`, no `..`). The fence and generation-scoped keys already bound the blast radius of a stale worker; this would additionally contain a buggy or hostile one.

## Alternatives considered
- Kafka for both flows (today): rejected; one unacked message per partition and no per-message redelivery or dead-lettering, so a work queue would have to be rebuilt on top of a log.
- RabbitMQ for both flows: rejected; lifecycle needs retention, replay, and independent consumers.
- RabbitMQ dispatch without the ownership API: rejected; a broker tracks messages, not task state, so a redelivered message could still double-run a task.
- Ownership calls as events instead of gRPC: rejected; a claim needs a synchronous win-or-lose answer and a renewal needs a bounded round trip. Kafka is never the heartbeat.
- A classic queue instead of quorum: rejected as the default; a quorum queue keeps unacked dispatches across a broker node failure.
- Nacking straight to the dead-letter queue (no requeue) instead of bounded redelivery: rejected; a brief task-service blip would dead-letter every in-flight dispatch on its first failure, and the reconciler would then carry all recovery at reconcile-interval latency. Bounded in-queue redelivery absorbs transient failures quickly and still dead-letters a genuine poison message, with the reconciler as the backstop.

## Not decided here
How pause and cancel reach the owning replica (they stay on Kafka for now); how the DLQ is drained; lifecycle dedup and replay (PR4).
