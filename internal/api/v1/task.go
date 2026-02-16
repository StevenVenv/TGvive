package v1

import (
	"strconv"
	"strings"

	"my-go-server/internal/engine"
	"my-go-server/internal/model"
	"my-go-server/internal/service"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
)

type TaskApi struct{}

// TaskActionReq 任务操作请求
type TaskActionReq struct {
	ID     uint   `json:"id" binding:"required"`
	Action string `json:"action" binding:"required,oneof=start stop pause"`
}

type CreateTaskReq struct {
	SourceURL  string `json:"source_url" binding:"required"`
	TargetURL  string `json:"target_url" binding:"required"`
	SessionKey string `json:"session_key" binding:"required"`
	StrategyID uint   `json:"strategy_id" binding:"required"`
}

// CreateTask 创建搬运任务
func (a *TaskApi) CreateTask(c *gin.Context) {
	var req CreateTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("配置参数格式错误: "+err.Error(), c)
		return
	}

	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	req.SourceURL = strings.TrimSpace(req.SourceURL)
	req.TargetURL = strings.TrimSpace(req.TargetURL)
	req.SessionKey = strings.TrimSpace(req.SessionKey)

	if req.SourceURL == "" || req.TargetURL == "" || req.SessionKey == "" || req.StrategyID == 0 {
		app.FailWithMsg("创建参数不完整", c)
		return
	}

	strategy, err := service.GetStrategyByID(userID, req.StrategyID)
	if err != nil {
		app.FailWithMsg("策略不存在或无权操作: "+err.Error(), c)
		return
	}

	task := model.Task{
		UserID:     userID,
		SourceURL:  req.SourceURL,
		TargetURL:  req.TargetURL,
		ExecuteBy:  req.SessionKey,
		StrategyID: req.StrategyID,

		CloneMode:    strategy.CloneMode,
		ContentTypes: strategy.ContentTypes,

		ScopeType:  strategy.ScopeType,
		ScopeValue: strings.TrimSpace(strategy.ScopeValue),

		KeepReply:    strategy.KeepReply,
		Realtime:     strategy.Realtime,
		CloneComment: strategy.CloneComment,
		GpuAccel:     strategy.GpuAccel,
		ChangeMD5:    strategy.ChangeMD5,

		DelayMinMs: strategy.DelayMinMs,
		DelayMaxMs: strategy.DelayMaxMs,

		DailyLimit: strategy.DailyLimit,
		RunWindow:  strings.TrimSpace(strategy.RunWindow),

		Status: model.TaskStatusStopped,

		HistoryOrder: strategy.HistoryOrder,
	}

	if task.CloneMode == 0 {
		task.CloneMode = 3
	}
	if task.ScopeType == 0 {
		task.ScopeType = 1
	}
	if task.HistoryOrder == 0 {
		task.HistoryOrder = 1
	}
	if task.DelayMinMs < 0 {
		task.DelayMinMs = 0
	}
	if task.DelayMaxMs < task.DelayMinMs {
		task.DelayMaxMs = task.DelayMinMs
	}
	if task.DailyLimit < 0 {
		task.DailyLimit = 0
	}

	if err := service.CreateTask(&task); err != nil {
		app.FailWithMsg("任务保存失败: "+err.Error(), c)
		return
	}

	app.OkWithData(task, c)
}

// GetTaskList 获取任务列表
func (a *TaskApi) GetTaskList(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	list, err := service.GetTaskList(userID)
	if err != nil {
		app.FailWithMsg("获取任务列表失败: "+err.Error(), c)
		return
	}
	app.OkWithData(list, c)
}

// UpdateTaskStatus 改变任务运行状态
func (a *TaskApi) UpdateTaskStatus(c *gin.Context) {
	var req TaskActionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("操作指令无效: "+err.Error(), c)
		return
	}

	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	task, err := service.ApplyTaskAction(userID, req.ID, req.Action)
	if err != nil {
		app.FailWithMsg("状态更新失败: "+err.Error(), c)
		return
	}

	app.OkWithData(gin.H{"status": task.Status, "msg": "指令已发送"}, c)
}

// GetTaskProgress 获取任务实时进度（模拟）
func (a *TaskApi) GetTaskProgress(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}

	idStr := c.Param("id")
	idU64, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || idU64 == 0 {
		app.FailWithMsg("任务ID不合法", c)
		return
	}

	task, err := service.GetTaskByID(userID, uint(idU64))
	if err != nil {
		app.FailWithMsg("任务不存在或无权操作: "+err.Error(), c)
		return
	}

	progress := engine.Manager.GetTaskProgress(task)
	app.OkWithData(progress, c)
}

func getCurrentUserID(c *gin.Context) uint {
	if v, ok := c.Get("user_id"); ok {
		switch vv := v.(type) {
		case uint:
			return vv
		case int:
			if vv >= 0 {
				return uint(vv)
			}
		case int64:
			if vv >= 0 {
				return uint(vv)
			}
		case float64:
			if vv >= 0 {
				return uint(vv)
			}
		case string:
			u64, err := strconv.ParseUint(vv, 10, 64)
			if err == nil {
				return uint(u64)
			}
		}
	}

	// Local/single-user fallback when auth middleware is disabled.
	return 1
}
