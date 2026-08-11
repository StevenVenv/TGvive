package v1

import (
	"testing"

	"my-go-server/internal/engine"
	"my-go-server/internal/model"
)

func TestApplyRuntimeStrategyToTaskUsesEffectiveStrategyFields(t *testing.T) {
	strategy := &model.Strategy{
		CloneMode:      3,
		AllowedTypes:   model.CSVStringSlice("video,image"),
		ScopeType:      2,
		ScopeValue:     " 100 ",
		HistoryOrder:   model.HistoryOrderNewToOld,
		KeepReply:      true,
		EnableRealtime: true,
		CloneComment:   true,
		ChangeMD5:      true,
		RandomFilename: true,
		DelayMinMs:     1000,
		DelayMaxMs:     3000,
	}
	strategy.ID = 777
	engine.GlobalStrategyCache.Set(strategy)
	defer engine.GlobalStrategyCache.Invalidate(int64(strategy.ID))

	task := model.Task{
		StrategyID:      strategy.ID,
		CloneMode:       1,
		ContentTypes:    model.CSVStringSlice("text"),
		ScopeType:       2,
		ScopeValue:      "10",
		HistoryOrder:    model.HistoryOrderOldToNew,
		KeepReply:       false,
		Realtime:        false,
		CloneComment:    false,
		ChangeMD5:       false,
		RandomFilename:  false,
		DelayMinMs:      0,
		DelayMaxMs:      0,
		HistoryCursor:   34,
		ProgressFailCnt: 5,
	}

	got := applyRuntimeStrategyToTask(task)

	if got.ScopeType != 2 || got.ScopeValue != "100" {
		t.Fatalf("scope=(%d,%q), want (2,%q)", got.ScopeType, got.ScopeValue, "100")
	}
	if got.HistoryOrder != model.HistoryOrderNewToOld {
		t.Fatalf("HistoryOrder=%d, want new-to-old", got.HistoryOrder)
	}
	if !got.KeepReply || !got.Realtime || !got.CloneComment || !got.ChangeMD5 || !got.RandomFilename {
		t.Fatalf("strategy switches were not applied: %+v", got)
	}
	if got.DelayMinMs != 1000 || got.DelayMaxMs != 3000 {
		t.Fatalf("delay fields were not applied: %+v", got)
	}
	if got.HistoryCursor != 34 || got.ProgressFailCnt != 5 {
		t.Fatalf("task runtime counters should be preserved: %+v", got)
	}
}
