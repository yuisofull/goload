package rabbitmq

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
)

func TestLoggerAdapter_LogsLevelMessageAndSortedFields(t *testing.T) {
	var out strings.Builder
	logger := loggerAdapter{log.NewLogfmtLogger(&out)}

	logger.Info("Subscribed", logFields{"topic": "task.created", "amqp_queue_name": "task.dispatch"})
	logger.Debug("Message published", nil)

	assert.Equal(t, "level=info msg=Subscribed amqp_queue_name=task.dispatch topic=task.created\n"+
		"level=debug msg=\"Message published\"\n", out.String())
}

func TestLoggerAdapter_ErrorCarriesTheError(t *testing.T) {
	var out strings.Builder
	logger := loggerAdapter{log.NewLogfmtLogger(&out)}

	logger.Error("Cannot reconnect", errors.New("connection refused"), logFields{"topic": "task.created"})

	want := "level=error msg=\"Cannot reconnect\" err=\"connection refused\" topic=task.created\n"
	assert.Equal(t, want, out.String())
}
