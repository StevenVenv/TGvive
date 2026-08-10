package engine

import "testing"

func TestParseTelegramPeerRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		kind      telegramPeerRefKind
		username  string
		channelID int64
		chatID    int64
		botChatID string
		wantOK    bool
		wantErr   bool
	}{
		{name: "at username", raw: "@telegram", kind: telegramPeerRefUsername, username: "telegram", wantOK: true},
		{name: "bare username", raw: "telegram", kind: telegramPeerRefUsername, username: "telegram", wantOK: true},
		{name: "tme username", raw: "https://t.me/telegram", kind: telegramPeerRefUsername, username: "telegram", wantOK: true},
		{name: "telegram me username message", raw: "telegram.me/telegram/123?single", kind: telegramPeerRefUsername, username: "telegram", wantOK: true},
		{name: "public preview username", raw: "https://t.me/s/telegram/123", kind: telegramPeerRefUsername, username: "telegram", wantOK: true},
		{name: "tg resolve", raw: "tg://resolve?domain=telegram&post=123", kind: telegramPeerRefUsername, username: "telegram", wantOK: true},
		{name: "bot channel id", raw: "-100123456789", kind: telegramPeerRefChannelID, channelID: 123456789, botChatID: "-100123456789", wantOK: true},
		{name: "private web post", raw: "https://t.me/c/123456789/42", kind: telegramPeerRefChannelID, channelID: 123456789, botChatID: "-100123456789", wantOK: true},
		{name: "private deep link", raw: "tg://privatepost?channel=123456789&post=42", kind: telegramPeerRefChannelID, channelID: 123456789, botChatID: "-100123456789", wantOK: true},
		{name: "private deep link bot id", raw: "tg://privatepost?channel=-100123456789&post=42", kind: telegramPeerRefChannelID, channelID: 123456789, botChatID: "-100123456789", wantOK: true},
		{name: "basic group id", raw: "-123456789", kind: telegramPeerRefGroupID, chatID: 123456789, botChatID: "-123456789", wantOK: true},
		{name: "positive numeric id is not username", raw: "123456789", wantOK: false},
		{name: "not a peer", raw: "not a valid peer", wantOK: false},
		{name: "invite web link", raw: "https://t.me/+abcdef", wantOK: true, wantErr: true},
		{name: "joinchat web link", raw: "https://t.me/joinchat/abcdef", wantOK: true, wantErr: true},
		{name: "bad private post id", raw: "https://t.me/c/notdigits/42", wantOK: true, wantErr: true},
		{name: "unsupported service link", raw: "https://t.me/share/url?url=https%3A%2F%2Fexample.com", wantOK: true, wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok, err := parseTelegramPeerRef(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (err=%v)", ok, tt.wantOK, err)
			}
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.wantOK {
				return
			}
			if got.Kind != tt.kind || got.Username != tt.username || got.ChannelID != tt.channelID || got.ChatID != tt.chatID || got.BotChatID != tt.botChatID {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestResolveBotChatID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "username", raw: "telegram", want: "@telegram"},
		{name: "at username", raw: "@telegram", want: "@telegram"},
		{name: "tme username", raw: "https://t.me/telegram/123", want: "@telegram"},
		{name: "tg resolve", raw: "tg://resolve?domain=telegram", want: "@telegram"},
		{name: "channel id", raw: "-100123456789", want: "-100123456789"},
		{name: "group id", raw: "-123456789", want: "-123456789"},
		{name: "positive bot chat id", raw: "123456789", want: "123456789"},
		{name: "private post", raw: "https://t.me/c/123456789/42", want: "-100123456789"},
		{name: "invite", raw: "https://t.me/+abcdef", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveBotChatID(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
