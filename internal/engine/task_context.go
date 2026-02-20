package engine

import (
	"context"
	"strings"
)

type taskRunCtxKey struct{}

type taskRunCtx struct {
	taskID uint
	runID  uint64
}

func withTaskRun(ctx context.Context, taskID uint, runID uint64) context.Context {
	if ctx == nil || taskID == 0 || runID == 0 {
		return ctx
	}
	return context.WithValue(ctx, taskRunCtxKey{}, taskRunCtx{taskID: taskID, runID: runID})
}

func taskRunFromCtx(ctx context.Context) (taskID uint, runID uint64, ok bool) {
	if ctx == nil {
		return 0, 0, false
	}
	v := ctx.Value(taskRunCtxKey{})
	tr, ok := v.(taskRunCtx)
	if !ok || tr.taskID == 0 || tr.runID == 0 {
		return 0, 0, false
	}
	return tr.taskID, tr.runID, true
}

func recordTaskDetailFromCtx(ctx context.Context, msg string) {
	taskID, runID, ok := taskRunFromCtx(ctx)
	if !ok {
		return
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	if Manager != nil {
		Manager.recordDetail(taskID, runID, msg)
	}
}
