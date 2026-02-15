package engine

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	qrcode "rsc.io/qr"
)

type QRState struct {
	SessionID string `json:"session_id"`
	Key       string `json:"key,omitempty"`
	URL       string `json:"url,omitempty"`
	Image     string `json:"image,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

const (
	QRStatusCreated    = "created"
	QRStatusPending    = "pending"
	QRStatusScanned    = "scanned"
	QRStatusAuthorized = "authorized"
	QRStatusExpired    = "expired"
	QRStatusError      = "error"
)

type qrHub struct {
	mu     sync.RWMutex
	states map[string]QRState
	subs   map[string]map[chan QRState]struct{}
}

var qr = newQRHub()

func newQRHub() *qrHub {
	return &qrHub{
		states: make(map[string]QRState),
		subs:   make(map[string]map[chan QRState]struct{}),
	}
}

func InitQRSession(sessionID string) QRState {
	st := QRState{
		SessionID: sessionID,
		Status:    QRStatusCreated,
		UpdatedAt: time.Now().Unix(),
	}
	qr.publish(sessionID, st)
	return st
}

func GetQRState(sessionID string) (QRState, bool) {
	return qr.get(sessionID)
}

func SubscribeQR(sessionID string) (<-chan QRState, func()) {
	return qr.subscribe(sessionID)
}

func (h *qrHub) publish(sessionID string, st QRState) {
	st.SessionID = sessionID
	st.UpdatedAt = time.Now().Unix()

	h.mu.Lock()
	if prev, ok := h.states[sessionID]; ok {
		if st.Key == "" {
			st.Key = prev.Key
		}
		if st.URL == "" {
			st.URL = prev.URL
		}
		if st.Image == "" {
			st.Image = prev.Image
		}
		if st.ExpiresAt == 0 {
			st.ExpiresAt = prev.ExpiresAt
		}
		if st.Error == "" && (st.Status == QRStatusError || st.Status == QRStatusExpired) {
			st.Error = prev.Error
		}
		if st.Status != QRStatusError && st.Status != QRStatusExpired {
			st.Error = ""
		}
	}
	h.states[sessionID] = st

	var subs []chan QRState
	if m, ok := h.subs[sessionID]; ok {
		subs = make([]chan QRState, 0, len(m))
		for ch := range m {
			subs = append(subs, ch)
		}
	}
	h.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- st:
		default:
		}
	}
}

func (h *qrHub) get(sessionID string) (QRState, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	st, ok := h.states[sessionID]
	return st, ok
}

func (h *qrHub) subscribe(sessionID string) (<-chan QRState, func()) {
	ch := make(chan QRState, 8)

	h.mu.Lock()
	m := h.subs[sessionID]
	if m == nil {
		m = make(map[chan QRState]struct{})
		h.subs[sessionID] = m
	}
	m[ch] = struct{}{}
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		if m := h.subs[sessionID]; m != nil {
			delete(m, ch)
			if len(m) == 0 {
				delete(h.subs, sessionID)
			}
		}
		h.mu.Unlock()
	}

	return ch, unsub
}

func (m *TaskManager) StartQRAuth(ctx context.Context, sessionID string) error {
	return m.StartQRAuthForKey(ctx, sessionID, "qr_"+sessionID)
}

func (m *TaskManager) StartQRAuthForKey(ctx context.Context, sessionID, key string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session_id is required")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("session key is required")
	}

	app, ok := Apps["desktop"]
	if !ok || app.ID == 0 || strings.TrimSpace(app.Hash) == "" {
		return errors.New("builtin app config missing")
	}

	sessionPath := GetSessionPathForKey(key)
	qr.publish(sessionID, QRState{
		Key:    key,
		Status: QRStatusPending,
	})

	d := tg.NewUpdateDispatcher()
	loggedIn := make(chan struct{}, 1)
	d.OnLoginToken(func(ctx context.Context, e tg.Entities, update *tg.UpdateLoginToken) error {
		qr.publish(sessionID, QRState{
			Key:    key,
			Status: QRStatusScanned,
		})
		select {
		case loggedIn <- struct{}{}:
		default:
		}
		return nil
	})

	client := telegram.NewClient(app.ID, app.Hash, telegram.Options{
		SessionStorage: &FileSessionStorage{Path: sessionPath},
		UpdateHandler:  d,
	})

	err := client.Run(ctx, func(ctx context.Context) error {
		if status, err := client.Auth().Status(ctx); err == nil && status.Authorized {
			qr.publish(sessionID, QRState{
				Key:    key,
				Status: QRStatusAuthorized,
			})
			return nil
		}

		_, err := client.QR().Auth(ctx, loggedIn, func(ctx context.Context, token qrlogin.Token) error {
			img := ""
			if code, err := qrcode.Encode(token.URL(), qrcode.M); err == nil {
				img = "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG())
			}
			qr.publish(sessionID, QRState{
				Key:       key,
				URL:       token.URL(),
				Image:     img,
				Status:    QRStatusPending,
				ExpiresAt: token.Expires().Unix(),
			})
			return nil
		})
		if err != nil {
			return err
		}

		qr.publish(sessionID, QRState{
			Key:    key,
			Status: QRStatusAuthorized,
		})
		return nil
	})

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			qr.publish(sessionID, QRState{
				Key:    key,
				Status: QRStatusExpired,
				Error:  fmt.Sprintf("context canceled: %v", err),
			})
			return err
		}
		qr.publish(sessionID, QRState{
			Key:    key,
			Status: QRStatusError,
			Error:  err.Error(),
		})
		return err
	}

	return nil
}
