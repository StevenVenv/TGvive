package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"my-go-server/internal/global"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

type CodeAuthState struct {
	SessionID string `json:"session_id"`
	Key       string `json:"key,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

const (
	CodeStatusCreated      = "created"
	CodeStatusNeedCode     = "need_code"
	CodeStatusNeedPassword = "need_password"
	CodeStatusAuthorized   = "authorized"
	CodeStatusExpired      = "expired"
	CodeStatusError        = "error"
)

type codeAuthSession struct {
	key   string
	phone string

	codeCh     chan string
	passwordCh chan string
}

type codeAuthHub struct {
	mu       sync.RWMutex
	states   map[string]CodeAuthState
	sessions map[string]*codeAuthSession
}

var codeAuth = newCodeAuthHub()

func newCodeAuthHub() *codeAuthHub {
	return &codeAuthHub{
		states:   make(map[string]CodeAuthState),
		sessions: make(map[string]*codeAuthSession),
	}
}

func InitCodeSession(sessionID, key, phone string) CodeAuthState {
	st := CodeAuthState{
		SessionID: strings.TrimSpace(sessionID),
		Key:       strings.TrimSpace(key),
		Phone:     strings.TrimSpace(phone),
		Status:    CodeStatusCreated,
		UpdatedAt: time.Now().Unix(),
	}

	codeAuth.mu.Lock()
	codeAuth.states[st.SessionID] = st
	codeAuth.sessions[st.SessionID] = &codeAuthSession{
		key:        st.Key,
		phone:      st.Phone,
		codeCh:     make(chan string, 1),
		passwordCh: make(chan string, 1),
	}
	codeAuth.mu.Unlock()

	return st
}

func GetCodeState(sessionID string) (CodeAuthState, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return CodeAuthState{}, false
	}
	codeAuth.mu.RLock()
	st, ok := codeAuth.states[sessionID]
	codeAuth.mu.RUnlock()
	return st, ok
}

func ProvideCode(sessionID, code string) error {
	sessionID = strings.TrimSpace(sessionID)
	code = strings.TrimSpace(code)
	if sessionID == "" {
		return errors.New("session_id is required")
	}
	if code == "" {
		return errors.New("code is required")
	}

	codeAuth.mu.RLock()
	s := codeAuth.sessions[sessionID]
	codeAuth.mu.RUnlock()
	if s == nil {
		return errors.New("session not found")
	}

	select {
	case s.codeCh <- code:
	default:
		select {
		case <-s.codeCh:
		default:
		}
		s.codeCh <- code
	}
	return nil
}

func ProvidePassword(sessionID, password string) error {
	sessionID = strings.TrimSpace(sessionID)
	password = strings.TrimSpace(password)
	if sessionID == "" {
		return errors.New("session_id is required")
	}
	if password == "" {
		return errors.New("password is required")
	}

	codeAuth.mu.RLock()
	s := codeAuth.sessions[sessionID]
	codeAuth.mu.RUnlock()
	if s == nil {
		return errors.New("session not found")
	}

	select {
	case s.passwordCh <- password:
	default:
		select {
		case <-s.passwordCh:
		default:
		}
		s.passwordCh <- password
	}
	return nil
}

func publishCodeState(sessionID string, st CodeAuthState) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	st.SessionID = sessionID
	st.UpdatedAt = time.Now().Unix()

	codeAuth.mu.Lock()
	if prev, ok := codeAuth.states[sessionID]; ok {
		if st.Key == "" {
			st.Key = prev.Key
		}
		if st.Phone == "" {
			st.Phone = prev.Phone
		}
		if st.Error == "" && st.Status == CodeStatusError {
			st.Error = prev.Error
		}
		if st.Status != CodeStatusError && st.Status != CodeStatusExpired {
			st.Error = ""
		}
	}
	codeAuth.states[sessionID] = st
	if st.Status == CodeStatusAuthorized || st.Status == CodeStatusError || st.Status == CodeStatusExpired {
		delete(codeAuth.sessions, sessionID)
	}
	codeAuth.mu.Unlock()
}

func pickAuthApp() (int, string, error) {
	apiID := global.Config.Telegram.APIID
	apiHash := strings.TrimSpace(global.Config.Telegram.APIHash)
	if apiID != 0 && apiHash != "" {
		return apiID, apiHash, nil
	}

	app, ok := Apps["desktop"]
	if ok && app.ID != 0 && strings.TrimSpace(app.Hash) != "" {
		return app.ID, strings.TrimSpace(app.Hash), nil
	}

	return 0, "", fmt.Errorf("telegram 配置缺失: telegram.api_id / telegram.api_hash")
}

type apiCodeAuth struct {
	sessionID string
	key       string
	phone     string
	codeCh    <-chan string
	passCh    <-chan string
}

func (a apiCodeAuth) Phone(ctx context.Context) (string, error) {
	return a.phone, nil
}

func (a apiCodeAuth) Password(ctx context.Context) (string, error) {
	publishCodeState(a.sessionID, CodeAuthState{Key: a.key, Phone: a.phone, Status: CodeStatusNeedPassword})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case p := <-a.passCh:
		p = strings.TrimSpace(p)
		if p == "" {
			return "", errors.New("empty password")
		}
		return p, nil
	}
}

func (a apiCodeAuth) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	return errors.New("sign up is not supported")
}

func (a apiCodeAuth) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign up is not supported")
}

func (a apiCodeAuth) Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
	_ = sentCode
	publishCodeState(a.sessionID, CodeAuthState{Key: a.key, Phone: a.phone, Status: CodeStatusNeedCode})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case code := <-a.codeCh:
		code = strings.TrimSpace(code)
		if code == "" {
			return "", errors.New("empty code")
		}
		return code, nil
	}
}

func (m *TaskManager) StartCodeAuth(ctx context.Context, sessionID, key, phone string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session_id is required")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("session key is required")
	}
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return errors.New("phone is required")
	}

	codeAuth.mu.RLock()
	s := codeAuth.sessions[sessionID]
	codeAuth.mu.RUnlock()
	if s == nil {
		return errors.New("session not found")
	}

	apiID, apiHash, err := pickAuthApp()
	if err != nil {
		publishCodeState(sessionID, CodeAuthState{Key: key, Phone: phone, Status: CodeStatusError, Error: err.Error()})
		return err
	}

	sessionPath := GetSessionPathForKey(key)
	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: &FileSessionStorage{Path: sessionPath},
	})

	a := apiCodeAuth{
		sessionID: sessionID,
		key:       key,
		phone:     phone,
		codeCh:    s.codeCh,
		passCh:    s.passwordCh,
	}

	err = client.Run(ctx, func(ctx context.Context) error {
		if status, err := client.Auth().Status(ctx); err == nil && status.Authorized {
			publishCodeState(sessionID, CodeAuthState{Key: key, Phone: phone, Status: CodeStatusAuthorized})
			return nil
		}

		flow := auth.NewFlow(a, auth.SendCodeOptions{})
		if err := flow.Run(ctx, client.Auth()); err != nil {
			return err
		}

		publishCodeState(sessionID, CodeAuthState{Key: key, Phone: phone, Status: CodeStatusAuthorized})
		return nil
	})

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			publishCodeState(sessionID, CodeAuthState{Key: key, Phone: phone, Status: CodeStatusExpired, Error: err.Error()})
			return err
		}
		publishCodeState(sessionID, CodeAuthState{Key: key, Phone: phone, Status: CodeStatusError, Error: err.Error()})
		return err
	}
	return nil
}
