package v1

import (
	"strconv"
	"time"

	"my-go-server/internal/global"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type DashboardApi struct{}

type dashboardWSMessage struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

func (a *DashboardApi) Summary(c *gin.Context) {
	global.StartMonitor()
	app.OkWithData(global.Stats.Snapshot(), c)
}

func (a *DashboardApi) Events(c *gin.Context) {
	// Deprecated: kept for compatibility with earlier polling UI.
	afterID, _ := strconv.ParseUint(c.Query("after_id"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	app.OkWithData(global.Stats.SnapshotLogs(afterID, limit), c)
}

func (a *DashboardApi) DashboardWS(c *gin.Context) {
	global.StartMonitor()

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	logSub := global.Stats.SubscribeLogs(160)
	defer global.Stats.UnsubscribeLogs(logSub)

	// Send initial snapshot + recent logs.
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(dashboardWSMessage{Type: "stats", Data: global.Stats.Snapshot()}); err != nil {
		return
	}
	for _, ev := range global.Stats.SnapshotLogs(0, 50) {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(dashboardWSMessage{Type: "log", Data: ev}); err != nil {
			return
		}
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

	statsTick := time.NewTicker(1 * time.Second)
	defer statsTick.Stop()

	pingTick := time.NewTicker(30 * time.Second)
	defer pingTick.Stop()

	for {
		select {
		case <-done:
			return
		case <-statsTick.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(dashboardWSMessage{Type: "stats", Data: global.Stats.Snapshot()}); err != nil {
				return
			}
		case ev, ok := <-logSub:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(dashboardWSMessage{Type: "log", Data: ev}); err != nil {
				return
			}
		case <-pingTick.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
