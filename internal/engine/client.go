package engine

import (
	"context"
	"errors"
	"strings"

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

	apiID, apiHash, err := pickTelegramApp()
	if err != nil {
		return nil, err
	}

	sessionPath := GetSessionPath(phone)

	client, err := newTelegramClient(apiID, apiHash, sessionPath, nil)
	if err != nil {
		return nil, err
	}

	return &TGClient{
		Client:      client,
		API:         client.API(),
		Phone:       phone,
		SessionPath: sessionPath,
	}, nil
}
