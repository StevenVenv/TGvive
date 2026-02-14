package engine

import (
	"context"
	"errors"
	"strings"

	"my-go-server/internal/global"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

type TGClient struct {
	Client      *telegram.Client
	API         *tg.Client
	Phone       string
	SessionPath string
}

func NewTGClient(ctx context.Context, phone string) (*TGClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	phone = strings.TrimSpace(phone)
	if phone == "" {
		return nil, errors.New("phone is required")
	}

	apiID := global.Config.Telegram.APIID
	apiHash := strings.TrimSpace(global.Config.Telegram.APIHash)
	if apiID == 0 || apiHash == "" {
		return nil, errors.New("telegram config missing: telegram.api_id / telegram.api_hash")
	}

	sessionPath := GetSessionPath(phone)

	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: &FileSessionStorage{Path: sessionPath},
	})

	return &TGClient{
		Client:      client,
		API:         client.API(),
		Phone:       phone,
		SessionPath: sessionPath,
	}, nil
}
