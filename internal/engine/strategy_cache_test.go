package engine

import (
	"testing"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"gorm.io/datatypes"
)

func TestStrategyCache_SetGetInvalidate(t *testing.T) {
	oldDB := global.DB
	global.DB = nil
	defer func() {
		global.DB = oldDB
	}()

	c := NewStrategyCache()

	orig := model.Strategy{}
	orig.ID = 123
	orig.Name = "orig"
	orig.ScheduleRules = datatypes.JSON([]byte(`[{"start":"10:00","end":"11:00","limit":2}]`))
	origRulesCopy := make([]byte, len(orig.ScheduleRules))
	copy(origRulesCopy, orig.ScheduleRules)

	c.Set(&orig)

	// Mutate the original instance after Set; cache should keep an internal copy.
	orig.Name = "mutated"
	if len(orig.ScheduleRules) > 0 {
		orig.ScheduleRules[0] = 'X'
	}

	got := c.Get(123)
	if got == nil {
		t.Fatalf("expected cache hit")
	}
	if got.Name != "orig" {
		t.Fatalf("expected Name=%q, got %q", "orig", got.Name)
	}
	if string(got.ScheduleRules) != string(origRulesCopy) {
		t.Fatalf("expected ScheduleRules deep-copied, got %q", string(got.ScheduleRules))
	}

	c.Invalidate(123)
	if got := c.Get(123); got != nil {
		t.Fatalf("expected nil after invalidate when DB is nil")
	}
}

func TestMergeHotFieldsIntoTaskUsesCurrentStrategyConfig(t *testing.T) {
	task := model.Task{
		StrategyID:       1,
		CloneMode:        1,
		ContentTypes:     model.CSVStringSlice("text"),
		BlockFileExts:    model.CSVStringSlice(".exe"),
		AllowFileExts:    model.CSVStringSlice(".txt"),
		ScopeType:        2,
		ScopeValue:       "10",
		HistoryOrder:     model.HistoryOrderOldToNew,
		KeepReply:        false,
		Realtime:         false,
		CloneComment:     false,
		GpuAccel:         false,
		ChangeMD5:        false,
		RandomFilename:   false,
		EnableMediaEdit:  true,
		DelayMinMs:       100,
		DelayMaxMs:       200,
		DailyLimit:       5,
		RunWindow:        "09:00-10:00",
		HistoryCursor:    34,
		HistoryMaxID:     34,
		SourceChannelID:  123,
		KeywordProfileID: 7,
	}
	strategy := &model.Strategy{
		CloneMode:       3,
		AllowedTypes:    model.CSVStringSlice("video,image"),
		BlockFileExts:   model.CSVStringSlice(".apk"),
		AllowFileExts:   model.CSVStringSlice(".zip"),
		ScopeType:       2,
		ScopeValue:      " 100 ",
		HistoryOrder:    model.HistoryOrderNewToOld,
		KeepReply:       true,
		EnableRealtime:  true,
		CloneComment:    true,
		GpuAccel:        true,
		ChangeMD5:       true,
		RandomFilename:  true,
		EnableMediaEdit: true,
		DelayMinMs:      1000,
		DelayMaxMs:      3000,
		DailyLimit:      0,
		RunWindow:       "",
	}

	got := MergeHotFieldsIntoTask(task, strategy)

	if got.CloneMode != 3 {
		t.Fatalf("CloneMode=%d, want 3", got.CloneMode)
	}
	if got.ContentTypes != strategy.AllowedTypes {
		t.Fatalf("ContentTypes=%q, want %q", got.ContentTypes, strategy.AllowedTypes)
	}
	if got.BlockFileExts != strategy.BlockFileExts || got.AllowFileExts != strategy.AllowFileExts {
		t.Fatalf("file suffix rules not copied from strategy")
	}
	if got.ScopeType != 2 || got.ScopeValue != "100" {
		t.Fatalf("scope=(%d,%q), want (2,%q)", got.ScopeType, got.ScopeValue, "100")
	}
	if got.HistoryOrder != model.HistoryOrderNewToOld {
		t.Fatalf("HistoryOrder=%d, want new-to-old", got.HistoryOrder)
	}
	if !got.KeepReply || !got.Realtime || !got.CloneComment || !got.GpuAccel || !got.ChangeMD5 || !got.RandomFilename || !got.EnableMediaEdit {
		t.Fatalf("strategy switches were not applied: %+v", got)
	}
	if got.DelayMinMs != 1000 || got.DelayMaxMs != 3000 || got.DailyLimit != 0 || got.RunWindow != "" {
		t.Fatalf("runtime throttle/quota fields were not applied: %+v", got)
	}
	if got.HistoryCursor != 34 || got.HistoryMaxID != 34 || got.SourceChannelID != 123 || got.KeywordProfileID != 7 {
		t.Fatalf("task runtime state fields should be preserved: %+v", got)
	}
	if bounds := parseHistoryBounds(got); bounds.MaxMessages != 100 {
		t.Fatalf("MaxMessages=%d, want 100", bounds.MaxMessages)
	}
}

func TestMergeHotFieldsIntoTaskForcesSeparatedPublishToUpload(t *testing.T) {
	task := model.Task{PublishType: "bot", CloneMode: 1}
	strategy := &model.Strategy{CloneMode: 1, EnableMediaEdit: true}

	got := MergeHotFieldsIntoTask(task, strategy)

	if got.CloneMode != 3 {
		t.Fatalf("CloneMode=%d, want 3 for separated publish", got.CloneMode)
	}
	if !got.EnableMediaEdit {
		t.Fatalf("EnableMediaEdit should remain enabled after separated publish forces upload mode")
	}
}

func TestMergeHotFieldsIntoTaskDisablesMediaEditOutsideUploadMode(t *testing.T) {
	task := model.Task{CloneMode: 3}
	strategy := &model.Strategy{CloneMode: 1, EnableMediaEdit: true}

	got := MergeHotFieldsIntoTask(task, strategy)

	if got.CloneMode != 1 {
		t.Fatalf("CloneMode=%d, want 1", got.CloneMode)
	}
	if got.EnableMediaEdit {
		t.Fatalf("EnableMediaEdit should be disabled outside upload mode")
	}
}
