package engine

import (
	"slices"
	"testing"

	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
)

func TestShouldCloneComment_OwnerOnly(t *testing.T) {
	sourceChannelID := int64(12345)
	linkedChatID := int64(77777)

	ownerMsg := &tg.Message{}
	ownerMsg.SetFromID(&tg.PeerChannel{ChannelID: sourceChannelID})

	anonGroupMsg := &tg.Message{PeerID: &tg.PeerChannel{ChannelID: linkedChatID}}
	anonGroupMsg.SetFromID(&tg.PeerChannel{ChannelID: linkedChatID})

	trustedMsg := &tg.Message{}
	trustedMsg.SetFromID(&tg.PeerUser{UserID: 200})

	anonMsg := &tg.Message{}
	anonMsg.SetFromID(&tg.PeerUser{UserID: groupAnonymousBotID})

	strangerMsg := &tg.Message{}
	strangerMsg.SetFromID(&tg.PeerUser{UserID: 999})

	rule := model.CommentRule{
		FilterMode:     "owner_only",
		TrustedUserIDs: []int64{200},
		AllowAnonymous: false,
	}
	if !ShouldCloneComment(ownerMsg, sourceChannelID, rule) {
		t.Fatalf("expected owner message allowed")
	}
	if !ShouldCloneComment(anonGroupMsg, sourceChannelID, rule) {
		t.Fatalf("expected anonymous 'send as group' allowed (linked chat treated as official)")
	}
	missingFrom := &tg.Message{PeerID: &tg.PeerChannel{ChannelID: linkedChatID}}
	if !ShouldCloneComment(missingFrom, sourceChannelID, rule) {
		t.Fatalf("expected missing from_id treated as linked chat identity")
	}
	if !ShouldCloneComment(trustedMsg, sourceChannelID, rule) {
		t.Fatalf("expected trusted user allowed")
	}
	if ShouldCloneComment(anonMsg, sourceChannelID, rule) {
		t.Fatalf("expected anonymous admin denied when allow_anonymous=false")
	}
	if ShouldCloneComment(strangerMsg, sourceChannelID, rule) {
		t.Fatalf("expected stranger denied in owner_only mode")
	}

	rule.AllowAnonymous = true
	if !ShouldCloneComment(anonMsg, sourceChannelID, rule) {
		t.Fatalf("expected anonymous admin allowed when allow_anonymous=true")
	}

	rule.FilterMode = "whitelist" // alias
	if !ShouldCloneComment(ownerMsg, sourceChannelID, rule) {
		t.Fatalf("expected alias 'whitelist' to behave like owner_only")
	}
}

func TestShouldCloneComment_OwnerOrLinked(t *testing.T) {
	sourceChannelID := int64(12345)
	linkedChatID := int64(77777)

	// "Send as group" appears as GroupAnonymousBot in MTProto.
	sendAsGroup := &tg.Message{PeerID: &tg.PeerChannel{ChannelID: linkedChatID}}
	sendAsGroup.SetFromID(&tg.PeerUser{UserID: groupAnonymousBotID})

	stranger := &tg.Message{}
	stranger.SetFromID(&tg.PeerUser{UserID: 999})

	rule := model.CommentRule{
		FilterMode:     "owner_or_linked",
		TrustedUserIDs: nil,
		AllowAnonymous: false, // should be ignored in this mode
	}
	if !ShouldCloneComment(sendAsGroup, sourceChannelID, rule) {
		t.Fatalf("expected 'send as group' allowed in owner_or_linked mode")
	}
	if ShouldCloneComment(stranger, sourceChannelID, rule) {
		t.Fatalf("expected stranger denied in owner_or_linked mode")
	}
}

func TestShouldCloneComment_All(t *testing.T) {
	sourceChannelID := int64(12345)
	rule := model.CommentRule{FilterMode: "all"}

	msg := &tg.Message{}
	msg.SetFromID(&tg.PeerUser{UserID: 999})
	if !ShouldCloneComment(msg, sourceChannelID, rule) {
		t.Fatalf("expected all mode to allow any user")
	}

	// Even without from_id, "all" should pass.
	emptyFrom := &tg.Message{}
	if !ShouldCloneComment(emptyFrom, sourceChannelID, rule) {
		t.Fatalf("expected all mode to allow missing from_id")
	}
}

func TestNormalizeRuntimeCommentRule_AllowedTypesAliases(t *testing.T) {
	out := normalizeRuntimeCommentRule(model.CommentRule{
		AllowedTypes: []string{"photo", "document", "TEXT", "Video", "bad", "document"},
	})
	want := []string{"image", "file", "text", "video"}
	if !slices.Equal(out.AllowedTypes, want) {
		t.Fatalf("allowed_types mismatch: got=%v want=%v", out.AllowedTypes, want)
	}
}

func TestHitBlockKeywords(t *testing.T) {
	if !hitBlockKeywords("Hello BUY now", []string{"buy"}) {
		t.Fatalf("expected keyword hit (case-insensitive substring)")
	}
	if hitBlockKeywords("Hello world", []string{"buy"}) {
		t.Fatalf("expected no keyword hit")
	}
	if hitBlockKeywords("   ", []string{"buy"}) {
		t.Fatalf("expected empty text ignored")
	}
}
