package engine

import (
	"testing"

	"my-go-server/internal/model"
)

func TestApplyLatestHistoryStatePreservesStrategyHistoryOrder(t *testing.T) {
	task := model.Task{
		StrategyID:    1,
		HistoryOrder:  model.HistoryOrderNewToOld,
		HistoryCursor: 10,
		HistoryMaxID:  10,
	}
	latest := model.Task{
		HistoryOrder:  model.HistoryOrderOldToNew,
		HistoryCursor: 34,
		HistoryMaxID:  962,
	}

	got := applyLatestHistoryState(task, latest)

	if got.HistoryOrder != model.HistoryOrderNewToOld {
		t.Fatalf("HistoryOrder=%d, want strategy order %d", got.HistoryOrder, model.HistoryOrderNewToOld)
	}
	if got.HistoryCursor != 34 || got.HistoryMaxID != 962 {
		t.Fatalf("history progress not refreshed: %+v", got)
	}
}

func TestApplyLatestHistoryStateUsesStoredHistoryOrderWithoutStrategy(t *testing.T) {
	task := model.Task{HistoryOrder: model.HistoryOrderOldToNew}
	latest := model.Task{
		HistoryOrder:  model.HistoryOrderNewToOld,
		HistoryCursor: 34,
		HistoryMaxID:  962,
	}

	got := applyLatestHistoryState(task, latest)

	if got.HistoryOrder != model.HistoryOrderNewToOld {
		t.Fatalf("HistoryOrder=%d, want stored order %d", got.HistoryOrder, model.HistoryOrderNewToOld)
	}
}
