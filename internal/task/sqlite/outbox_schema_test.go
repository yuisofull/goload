package sqlite

import (
	"context"
	"testing"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"
	"github.com/stretchr/testify/require"
)

func TestEnsureOutboxSchema_CreatesTableAndIsIdempotent(t *testing.T) {
	pool := newTestPool(t)

	conn := pool.Get(context.Background())
	require.NotNil(t, conn)
	defer pool.Put(conn)

	// newTestPool already ran it once; pocket runs it on every start.
	require.NoError(t, EnsureOutboxSchema(conn))

	columns := map[string]string{}
	require.NoError(t, sqlitex.ExecuteTransient(conn, `PRAGMA table_info(task_outbox);`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			// table_info columns: cid, name, type, notnull, dflt_value, pk
			columns[stmt.ColumnText(1)] = stmt.ColumnText(2)
			return nil
		},
	}))
	require.Equal(t, map[string]string{
		"id":            "TEXT",
		"aggregate_id":  "INTEGER",
		"destination":   "TEXT",
		"event_type":    "TEXT",
		"payload":       "BLOB",
		"created_at":    "TEXT",
		"sent_at":       "TEXT",
		"trace_context": "TEXT",
	}, columns)

	var indexes []string
	require.NoError(t, sqlitex.ExecuteTransient(
		conn,
		`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'task_outbox_unsent_idx';`,
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				indexes = append(indexes, stmt.ColumnText(0))
				return nil
			},
		},
	))
	require.Equal(t, []string{"task_outbox_unsent_idx"}, indexes)
}
