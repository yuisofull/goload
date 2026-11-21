package rabbitmq

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testURL = "amqp://guest:guest@localhost:5672/"

func TestResolveConfig_AppliesDefaults(t *testing.T) {
	cfg, err := resolveConfig(&Config{URL: testURL})
	require.NoError(t, err)

	assert.Equal(t, Config{
		URL:                testURL,
		Exchange:           "goload.dispatch",
		Queue:              "task.dispatch",
		DeadLetterExchange: "goload.dispatch.dlx",
		DeadLetterQueue:    "task.dispatch.dlq",
		DeliveryLimit:      5,
		Prefetch:           4,
		ConfirmTimeout:     30 * time.Second,
		ReconnectDelay:     time.Second,
	}, cfg)
}

func TestResolveConfig_KeepsOverrides(t *testing.T) {
	want := Config{
		URL:                testURL,
		Exchange:           "custom.exchange",
		Queue:              "custom.queue",
		DeadLetterExchange: "custom.dead.exchange",
		DeadLetterQueue:    "custom.dead.queue",
		DeliveryLimit:      3,
		Prefetch:           16,
		ConfirmTimeout:     5 * time.Second,
		ReconnectDelay:     250 * time.Millisecond,
	}
	input := want

	got, err := resolveConfig(&input)
	require.NoError(t, err)

	assert.Equal(t, want, got)
}

func TestResolveConfig_DerivesDeadLetterNamesFromOverriddenNames(t *testing.T) {
	cfg, err := resolveConfig(&Config{URL: testURL, Exchange: "jobs", Queue: "jobs.work"})
	require.NoError(t, err)

	assert.Equal(t, "jobs.dlx", cfg.DeadLetterExchange)
	assert.Equal(t, "jobs.work.dlq", cfg.DeadLetterQueue)
}

func TestResolveConfig_NonPositiveTuningFallsBackToDefaults(t *testing.T) {
	cfg, err := resolveConfig(&Config{
		URL:            testURL,
		DeliveryLimit:  -1,
		Prefetch:       -1,
		ConfirmTimeout: -1,
		ReconnectDelay: -1,
	})
	require.NoError(t, err)

	assert.Equal(t, DefaultDeliveryLimit, cfg.DeliveryLimit)
	assert.Equal(t, DefaultPrefetch, cfg.Prefetch)
	assert.Equal(t, DefaultConfirmTimeout, cfg.ConfirmTimeout)
	assert.Equal(t, DefaultReconnectDelay, cfg.ReconnectDelay)
}

func TestResolveConfig_DoesNotMutateCallerConfig(t *testing.T) {
	input := Config{URL: testURL}

	_, err := resolveConfig(&input)
	require.NoError(t, err)

	assert.Equal(t, Config{URL: testURL}, input)
}

func TestResolveConfig_RejectsMissingURL(t *testing.T) {
	_, err := resolveConfig(nil)
	require.ErrorIs(t, err, ErrURLEmpty)

	_, err = resolveConfig(&Config{})
	require.ErrorIs(t, err, ErrURLEmpty)
}

func TestResolveConfig_RejectsInvalidURLWithoutLeakingCredentials(t *testing.T) {
	for _, url := range []string{
		"http://user:s3cret@localhost:5672/",
		"amqp://user:s3cret@localhost:notaport/",
		"amqp://user:s3cret@local host/",
	} {
		_, err := resolveConfig(&Config{URL: url})

		require.ErrorIs(t, err, ErrURLInvalid, url)
		assert.NotContains(t, err.Error(), "s3cret")
	}
}
