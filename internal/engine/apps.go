package engine

import (
	"errors"
	"strings"

	"my-go-server/internal/global"
)

const (
	AppBuiltin = "builtin"
	AppDesktop = "desktop"
	AppIOS     = "ios"
)

type App struct {
	ID   int
	Hash string
}

// Apps stores built-in app credentials.
// Note: Using 3rd-party/offical client credentials may violate Telegram ToS in some scenarios.
var Apps = map[string]App{
	// App created by iyear (tdl author).
	// Ref: https://github.com/iyear/tdl/blob/master/pkg/tclient/app.go
	AppBuiltin: {ID: 15055931, Hash: "021d433426cbb920eeb95164498fe3d3"},
	// Telegram Desktop (tdesktop).
	AppDesktop: {ID: 2040, Hash: "b18441a1ff607e10a989891a5462e627"},
	// Telegram iOS (legacy).
	AppIOS: {ID: 10840, Hash: "62d8fd6c4a51139158392157057cc61d"},
}

func pickTelegramApp() (int, string, error) {
	apiID := global.Config.Telegram.APIID
	apiHash := strings.TrimSpace(global.Config.Telegram.APIHash)
	// Treat sample placeholders as "not configured".
	if apiID == 1234567 && apiHash == "abcdefg123456" {
		apiID = 0
		apiHash = ""
	}
	if apiID > 0 && apiHash != "" {
		return apiID, apiHash, nil
	}

	if app, ok := Apps[AppBuiltin]; ok && app.ID > 0 && strings.TrimSpace(app.Hash) != "" {
		return app.ID, strings.TrimSpace(app.Hash), nil
	}
	if app, ok := Apps[AppDesktop]; ok && app.ID > 0 && strings.TrimSpace(app.Hash) != "" {
		return app.ID, strings.TrimSpace(app.Hash), nil
	}

	return 0, "", errors.New("builtin telegram app config missing")
}
