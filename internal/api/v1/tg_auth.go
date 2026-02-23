package v1

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"my-go-server/internal/engine"
	"my-go-server/internal/middleware"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type TGAuthApi struct{}

func (a *TGAuthApi) GetQRCode(c *gin.Context) {
	sessionID, err := newSessionID()
	if err != nil {
		app.FailWithMsg("生成会话ID失败: "+err.Error(), c)
		return
	}

	engine.InitQRSession(sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	go func() {
		defer cancel()
		_ = engine.Manager.StartQRAuth(ctx, sessionID)
	}()

	app.OkWithData(gin.H{"session_id": sessionID}, c)
}

func (a *TGAuthApi) CheckQRStatus(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		app.FailWithMsg("缺少 session_id", c)
		return
	}

	st, ok := engine.GetQRState(sessionID)
	if !ok {
		app.FailWithMsg("会话不存在", c)
		return
	}
	app.OkWithData(st, c)
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
	EnableCompression: true,
	CheckOrigin: func(r *http.Request) bool {
		origin := ""
		if r != nil {
			origin = r.Header.Get("Origin")
		}
		// Non-browser clients may omit Origin.
		if origin == "" {
			return true
		}
		return middleware.IsOriginAllowed(origin)
	},
}

func (a *TGAuthApi) QRWebSocket(c *gin.Context) {
	sessionID := c.Query("session_id")
	if sessionID == "" {
		app.FailWithMsg("缺少 session_id", c)
		return
	}

	if _, ok := engine.GetQRState(sessionID); !ok {
		app.FailWithMsg("会话不存在", c)
		return
	}

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Hardening: bound memory usage and enable keepalive via ping/pong.
	conn.SetReadLimit(64 * 1024)
	_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		return nil
	})

	updates, unsubscribe := engine.SubscribeQR(sessionID)
	defer unsubscribe()

	if st, ok := engine.GetQRState(sessionID); ok {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_ = conn.WriteJSON(st)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-done:
			return
		case st := <-updates:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(st); err != nil {
				return
			}
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
