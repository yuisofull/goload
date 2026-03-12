package main

import (
	"context"
	"errors"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/yuisofull/goload/internal/download"
	internalerrors "github.com/yuisofull/goload/internal/errors"
)

type loggingMiddleware struct {
	next   download.Service
	logger log.Logger
}

func (m *loggingMiddleware) logErr(method string, duration time.Duration, err error) {
	if err != nil {
		var svcErr *internalerrors.Error
		if errors.As(err, &svcErr) {
			level.Debug(m.logger).Log(
				"method", method,
				"duration", duration,
				"err", err,
				"code", svcErr.Code,
				"msg", "download service request failed with handled error",
			)
		} else {
			level.Error(m.logger).Log(
				"method", method,
				"duration", duration,
				"err", err,
				"msg", "download service request failed with internal error",
			)
		}
	} else {
		level.Info(m.logger).Log(
			"method", method,
			"duration", duration,
			"msg", "download service request handled successfully",
		)
	}
}

func (m *loggingMiddleware) ExecuteTask(ctx context.Context, req download.TaskRequest) error {
	start := time.Now()
	err := m.next.ExecuteTask(ctx, req)
	m.logErr("ExecuteTask", time.Since(start), err)
	return err
}

func (m *loggingMiddleware) PauseTask(ctx context.Context, taskID uint64) error {
	start := time.Now()
	err := m.next.PauseTask(ctx, taskID)
	m.logErr("PauseTask", time.Since(start), err)
	return err
}

func (m *loggingMiddleware) ResumeTask(ctx context.Context, taskID uint64) error {
	start := time.Now()
	err := m.next.ResumeTask(ctx, taskID)
	m.logErr("ResumeTask", time.Since(start), err)
	return err
}

func (m *loggingMiddleware) CancelTask(ctx context.Context, taskID uint64) error {
	start := time.Now()
	err := m.next.CancelTask(ctx, taskID)
	m.logErr("CancelTask", time.Since(start), err)
	return err
}

func (m *loggingMiddleware) StreamFile(ctx context.Context, req download.FileStreamRequest) (*download.FileStreamResponse, error) {
	start := time.Now()
	resp, err := m.next.StreamFile(ctx, req)
	m.logErr("StreamFile", time.Since(start), err)
	return resp, err
}

func (m *loggingMiddleware) GetActiveTaskCount(ctx context.Context) int {
	return m.next.GetActiveTaskCount(ctx)
}
