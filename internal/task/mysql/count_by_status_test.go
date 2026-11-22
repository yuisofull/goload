package mysql

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/task"
)

func TestTaskRepo_CountByStatus(t *testing.T) {
	fake, db := openFakeDB(t)
	fake.columns = []string{"status", "count"}
	fake.rows = [][]driver.Value{
		{"PENDING", int64(3)},
		{"FAILED", int64(1)},
	}

	sc := NewTaskRepo(db).(task.StatusCounter)
	got, err := sc.CountByStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[task.TaskStatus]int64{
		task.StatusPending: 3,
		task.StatusFailed:  1,
	}, got)
}
