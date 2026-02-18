package engine

import (
	"reflect"
	"testing"

	"github.com/gotd/td/tg"
)

func TestPlanMediaGroup_CaptionMigrationFromFilteredOutMedia(t *testing.T) {
	m := &TaskManager{}

	photoMsg := &tg.Message{
		ID:        10,
		GroupedID: 1,
		Media: &tg.MessageMediaPhoto{
			Photo: &tg.Photo{ID: 1, AccessHash: 2, FileReference: []byte{1}},
		},
	}

	captionEntities := []tg.MessageEntityClass{&tg.MessageEntityBold{Offset: 0, Length: 1}}
	videoMsg := &tg.Message{
		ID:        11,
		GroupedID: 1,
		Message:   "hello",
		Entities:  captionEntities,
		Media: &tg.MessageMediaDocument{
			Video: true,
			Document: &tg.Document{
				ID:            2,
				AccessHash:    3,
				FileReference: []byte{2},
				MimeType:      "video/mp4",
			},
		},
	}

	allowed := map[string]struct{}{"image": {}}
	plan := PlanMediaGroup(m, []*tg.Message{photoMsg, videoMsg}, allowed)

	if plan.Need != 1 {
		t.Fatalf("expected Need=1, got %d", plan.Need)
	}
	if plan.Text != nil {
		t.Fatalf("expected Text=nil, got non-nil")
	}
	if len(plan.Media) != 1 {
		t.Fatalf("expected Media=1, got %d", len(plan.Media))
	}
	if got := plan.Media[0].Message; got != "hello" {
		t.Fatalf("expected migrated caption %q, got %q", "hello", got)
	}
	if !reflect.DeepEqual(plan.Media[0].Entities, captionEntities) {
		t.Fatalf("expected entities migrated")
	}
	if plan.Skipped != 1 {
		t.Fatalf("expected Skipped=1, got %d", plan.Skipped)
	}
	if photoMsg.Message != "" {
		t.Fatalf("expected original photo message unchanged, got %q", photoMsg.Message)
	}
}

func TestPlanMediaGroup_DowngradeToTextWhenOnlyTextAllowed(t *testing.T) {
	m := &TaskManager{}

	photoMsg := &tg.Message{
		ID:        10,
		GroupedID: 1,
		Media: &tg.MessageMediaPhoto{
			Photo: &tg.Photo{ID: 1, AccessHash: 2, FileReference: []byte{1}},
		},
	}
	videoMsg := &tg.Message{
		ID:        11,
		GroupedID: 1,
		Message:   "hello",
		Media: &tg.MessageMediaDocument{
			Video: true,
			Document: &tg.Document{
				ID:            2,
				AccessHash:    3,
				FileReference: []byte{2},
				MimeType:      "video/mp4",
			},
		},
	}

	allowed := map[string]struct{}{"text": {}}
	plan := PlanMediaGroup(m, []*tg.Message{photoMsg, videoMsg}, allowed)

	if plan.Need != 1 {
		t.Fatalf("expected Need=1, got %d", plan.Need)
	}
	if plan.Text == nil {
		t.Fatalf("expected Text non-nil")
	}
	if got := plan.Text.Message; got != "hello" {
		t.Fatalf("expected downgraded text %q, got %q", "hello", got)
	}
	if plan.Text.Media != nil {
		t.Fatalf("expected downgraded Text.Media=nil")
	}
	if plan.Text.GroupedID != 0 {
		t.Fatalf("expected downgraded Text.GroupedID=0, got %d", plan.Text.GroupedID)
	}
	if len(plan.Media) != 0 {
		t.Fatalf("expected Media empty, got %d", len(plan.Media))
	}
	if plan.Skipped != 2 {
		t.Fatalf("expected Skipped=2, got %d", plan.Skipped)
	}
}

func TestPlanMediaGroup_DropWhenNoCaptionAndNoMediaKept(t *testing.T) {
	m := &TaskManager{}

	photoMsg := &tg.Message{
		ID:        10,
		GroupedID: 1,
		Media: &tg.MessageMediaPhoto{
			Photo: &tg.Photo{ID: 1, AccessHash: 2, FileReference: []byte{1}},
		},
	}
	videoMsg := &tg.Message{
		ID:        11,
		GroupedID: 1,
		Media: &tg.MessageMediaDocument{
			Video: true,
			Document: &tg.Document{
				ID:            2,
				AccessHash:    3,
				FileReference: []byte{2},
				MimeType:      "video/mp4",
			},
		},
	}

	allowed := map[string]struct{}{"text": {}}
	plan := PlanMediaGroup(m, []*tg.Message{photoMsg, videoMsg}, allowed)

	// Sanity: no captions in input.
	if plan.Caption != "" {
		t.Fatalf("expected Caption empty, got %q", plan.Caption)
	}
	if plan.Need != 0 {
		t.Fatalf("expected Need=0, got %d", plan.Need)
	}
	if plan.Text != nil {
		t.Fatalf("expected Text=nil")
	}
	if len(plan.Media) != 0 {
		t.Fatalf("expected Media empty, got %d", len(plan.Media))
	}
}
