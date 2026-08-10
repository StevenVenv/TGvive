package engine

import (
	"testing"

	"my-go-server/internal/model"
)

func TestInferTotalRealtimeIsUnknown(t *testing.T) {
	task := model.Task{
		Realtime:   true,
		ScopeType:  2,
		ScopeValue: "100",
		Status:     model.TaskStatusRunning,
	}

	if got := inferTotal(task); got != 0 {
		t.Fatalf("inferTotal realtime = %d, want 0", got)
	}
}

func TestSyncStateTotalClearsRealtimeCachedTotal(t *testing.T) {
	st := &taskState{Total: 100, Processed: 3}
	task := model.Task{
		Realtime:   true,
		ScopeType:  2,
		ScopeValue: "10",
		Status:     model.TaskStatusRunning,
	}

	syncStateTotalFromTask(task, st)

	if st.Total != 0 {
		t.Fatalf("realtime cached total = %d, want 0", st.Total)
	}
}

func TestSyncStateTotalLowersRunningEstimate(t *testing.T) {
	st := &taskState{Total: 100}
	task := model.Task{
		ScopeType:  2,
		ScopeValue: "10",
		Status:     model.TaskStatusRunning,
	}

	syncStateTotalFromTask(task, st)

	if st.Total != 10 {
		t.Fatalf("running cached total = %d, want 10", st.Total)
	}
}
