#!/usr/bin/env bash
# Creates one download through the compose stack and checks Jaeger holds it as
# one trace across gateway, task and download.
#
#   make compose-up && scripts/trace-smoke.sh
#   scripts/trace-smoke.sh https://example.com/
set -euo pipefail

GATEWAY=http://localhost:8081
JAEGER=http://127.0.0.1:16686
SOURCE_URL=${1:-https://example.com/}
TRACE_START=$(date -u +%FT%TZ)
ACCOUNT=trace-smoke-$RANDOM
PASSWORD=trace-smoke-password
TASK_WAIT_SECONDS=60
TRACE_WAIT_SECONDS=20

fail() { echo "FAIL: $*" >&2; exit 1; }

json() { python3 -c "import json,sys; print(json.load(sys.stdin)$1)"; }

curl -sf -X POST "$GATEWAY/api/v1/auth/create" -H 'Content-Type: application/json' \
  -d "{\"account_name\":\"$ACCOUNT\",\"password\":\"$PASSWORD\"}" >/dev/null || fail "create account"
TOKEN=$(curl -sf -X POST "$GATEWAY/api/v1/auth/session" -H 'Content-Type: application/json' \
  -d "{\"account_name\":\"$ACCOUNT\",\"password\":\"$PASSWORD\"}" | json "['token']") || fail "create session"

TASK_ID=$(curl -sf -X POST "$GATEWAY/api/v1/tasks/create" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"file_name\":\"smoke.bin\",\"source_url\":\"$SOURCE_URL\",\"source_type\":\"HTTPS\"}" \
  | json "['task']['id']") || fail "create task"
echo "task $TASK_ID created"

STATUS=UNKNOWN
for _ in $(seq "$TASK_WAIT_SECONDS"); do
  STATUS=$(curl -sf "$GATEWAY/api/v1/tasks/get?id=$TASK_ID" -H "Authorization: Bearer $TOKEN" \
    | json "['task']['status']" 2>/dev/null) || STATUS=UNKNOWN
  case "$STATUS" in
    COMPLETED) break ;;
    FAILED | CANCELLED) fail "task $TASK_ID is $STATUS" ;;
  esac
  sleep 1
done
[ "$STATUS" = COMPLETED ] || fail "task $TASK_ID is $STATUS after ${TASK_WAIT_SECONDS}s"
echo "task $TASK_ID completed"

check_trace() {
  curl -fsSG "$JAEGER/api/v3/traces" \
    --data-urlencode "query.service_name=download" \
    --data-urlencode "query.operation_name=process task.created" \
    --data-urlencode "query.start_time_min=$TRACE_START" \
    --data-urlencode "query.start_time_max=$(date -u +%FT%TZ)" \
    --data-urlencode "query.search_depth=200" |
    TASK_ID=$TASK_ID python3 -c '
import collections, json, os, sys

def string_attr(attrs, key):
    return next((a["value"].get("stringValue") for a in attrs if a["key"] == key), None)

found = set()
spans = collections.defaultdict(set)
for line in sys.stdin:
    if not line.strip():
        continue
    for resource in json.loads(line)["result"]["resourceSpans"]:
        service = string_attr(resource["resource"]["attributes"], "service.name")
        for scope in resource["scopeSpans"]:
            for span in scope["spans"]:
                trace_id = span["traceId"]
                spans[trace_id].add((service, span["name"]))
                if string_attr(span.get("attributes", []), "goload.task.id") == os.environ["TASK_ID"]:
                    found.add(trace_id)
if len(found) != 1:
    sys.exit(f"found {len(found)} traces for this task, expected one")
trace_id, = found
wanted = {("gateway", "POST /api/v1/tasks/create"), ("task", "publish task.created"),
          ("download", "process task.created"), ("download", "transfer"), ("download", "complete")}
missing = wanted - spans[trace_id]
if missing:
    sys.exit(f"trace {trace_id} missing {sorted(missing)}")
print(f"PASS: trace {trace_id} contains all five spans across gateway, task and download")
'
}

for _ in $(seq "$TRACE_WAIT_SECONDS"); do
  if RESULT=$(check_trace 2>&1); then
    echo "$RESULT"
    exit 0
  fi
  sleep 1
done
fail "$RESULT"
