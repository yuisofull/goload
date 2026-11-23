package sqlite

import (
	"errors"

	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"
)

// ownershipColumns are the task ownership columns in the order they are
// added. lease_expires_at and next_attempt_at hold fixed-width UTC text (see
// outboxTimeLayout).
var ownershipColumns = []addedColumn{
	{"owner_id", `ALTER TABLE tasks ADD COLUMN owner_id TEXT;`},
	{"lease_expires_at", `ALTER TABLE tasks ADD COLUMN lease_expires_at TEXT;`},
	{"attempt_generation", `ALTER TABLE tasks ADD COLUMN attempt_generation INTEGER NOT NULL DEFAULT 0;`},
	{"retry_count", `ALTER TABLE tasks ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0;`},
	{"next_attempt_at", `ALTER TABLE tasks ADD COLUMN next_attempt_at TEXT;`},
	{"result_content_type", `ALTER TABLE tasks ADD COLUMN result_content_type TEXT;`},
	{"result_file_size", `ALTER TABLE tasks ADD COLUMN result_file_size INTEGER NOT NULL DEFAULT 0;`},
}

// EnsureOwnershipSchema adds the task ownership columns (owner, lease,
// generation, retry state) to an existing tasks table and creates the index
// the reconciler reads through. It is idempotent; the pocket binaries call it
// on every start, after the tasks table is created. Existing rows read as
// unowned, generation 0, retry count 0.
func EnsureOwnershipSchema(conn *sqlite.Conn) error {
	existing, err := columnNames(conn, "tasks")
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return errors.New("sqlite: tasks table does not exist; create it before the ownership columns")
	}
	if err := addMissingColumns(conn, existing, ownershipColumns); err != nil {
		return err
	}

	// The reconciler looks for claimable tasks by status and lease expiry;
	// the status prefix lets it skip the finished tasks.
	return sqlitex.ExecuteTransient(
		conn,
		`CREATE INDEX IF NOT EXISTS tasks_reconcile_idx ON tasks (status, lease_expires_at);`,
		nil,
	)
}
