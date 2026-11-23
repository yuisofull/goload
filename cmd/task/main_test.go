package main

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskBinaryRegistersTheMySQLDriver(t *testing.T) {
	require.Contains(t, sql.Drivers(), "mysql")
}
