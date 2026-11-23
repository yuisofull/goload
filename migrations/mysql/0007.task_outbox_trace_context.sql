# Outbox trace context (observability, Feature 2).
# The W3C trace carrier (traceparent/tracestate as JSON) captured when the
# event was recorded, so the asynchronous relay continues the producer's trace.
# NULL for rows written before tracing; those still dispatch, as a new trace.
#
# Idempotent on purpose: run-migrations.sh applies every file on each start,
# and MySQL has no ADD COLUMN IF NOT EXISTS. The column is added through a
# prepared statement only when information_schema does not list it yet.

SET @exist := (
    SELECT COUNT(*)
    FROM information_schema.columns
    WHERE table_schema = DATABASE()
      AND table_name = 'task_outbox'
      AND column_name = 'trace_context'
);
SET @sqlstmt := IF(
    @exist = 0,
    'ALTER TABLE task_outbox ADD COLUMN trace_context TEXT NULL',
    'SELECT 1'
);
PREPARE stmt FROM @sqlstmt;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
