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
