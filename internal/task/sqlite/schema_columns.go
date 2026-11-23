package sqlite

import (
	sqlite "github.com/go-llsqlite/crawshaw"
	"github.com/go-llsqlite/crawshaw/sqlitex"
)

// addedColumn is a column added to a table after it first shipped. SQLite has
// no ADD COLUMN IF NOT EXISTS, so only the missing ones are added.
type addedColumn struct {
	name string
	ddl  string
}

func columnNames(conn *sqlite.Conn, table string) (map[string]bool, error) {
	names := map[string]bool{}
	err := sqlitex.ExecuteTransient(conn, `PRAGMA table_info(`+table+`);`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			// table_info columns: cid, name, type, notnull, dflt_value, pk
			names[stmt.ColumnText(1)] = true
			return nil
		},
	})
	return names, err
}

func addMissingColumns(conn *sqlite.Conn, existing map[string]bool, columns []addedColumn) error {
	for _, column := range columns {
		if existing[column.name] {
			continue
		}
		if err := sqlitex.ExecuteTransient(conn, column.ddl, nil); err != nil {
			return err
		}
	}
	return nil
}
