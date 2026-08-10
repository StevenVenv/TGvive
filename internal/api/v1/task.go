package v1

import (
	"strconv"
	"strings"

	"my-go-server/internal/engine"
	"my-go-server/internal/global"
	"my-go-server/internal/model"
	"my-go-server/internal/service"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type TaskApi struct{}

// TaskActionReq 任务操作请求
type TaskActionReq struct {
	ID     uint   `json:"id" binding:"required"`
	Action string `json:"action" binding:"required,oneof=start stop pause restart"`
}

type CreateTaskReq struct {
	SourceURL  string `json:"source_url" binding:"required"`
	TargetURL  string `json:"target_url" binding:"required"`
	SessionKey string `json:"session_key" binding:"required"` // crawler

	Remark string `json:"remark"`

	// PublishType: ""(same as crawler) | account | bot | same(alias)
	PublishType       string `json:"publish_type" binding:"omitempty,oneof=account bot same"`
	PublishSessionKey string `json:"publish_session_key"`
	PublishBotID      string `json:"publish_bot_id"`

	StrategyID       uint `json:"strategy_id" binding:"required"`
	KeywordProfileID uint `json:"keyword_profile_id"`
}

type UpdateTaskReq struct {
	SourceURL  string `json:"source_url" binding:"required"`
	TargetURL  string `json:"target_url" binding:"required"`
	SessionKey string `json:"session_key" binding:"required"` // crawler

	Remark string `json:"remark"`

	PublishType       string `json:"publish_type" binding:"omitempty,oneof=account bot same"`
	PublishSessionKey string `json:"publish_session_key"`
	PublishBotID      string `json:"publish_bot_id"`

	StrategyID       uint `json:"strategy_id" binding:"required"`
	KeywordProfileID uint `json:"keyword_profile_id"`
}

// CreateTask 创建转发任务
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
	req.Remark = strings.TrimSpace(req.Remark)
	req.PublishType = strings.TrimSpace(req.PublishType)
	req.PublishSessionKey = strings.TrimSpace(req.PublishSessionKey)
	req.PublishBotID = strings.TrimSpace(req.PublishBotID)

	if req.SourceURL == "" || req.TargetURL == "" || req.SessionKey == "" || req.StrategyID == 0 {
		app.FailWithMsg("创建参数不完整", c)
		return
	}

	// Canonicalize publish config.
	if req.PublishType == "same" {
		req.PublishType = ""
	}
	switch req.PublishType {
	case "":
		req.PublishSessionKey = ""
		req.PublishBotID = ""
	case "account":
		if req.PublishSessionKey == "" {
			app.FailWithMsg("发布账号不能为空", c)
			return
		}
		if req.PublishSessionKey == req.SessionKey {
			req.PublishType = ""
			req.PublishSessionKey = ""
			req.PublishBotID = ""
		}
		req.PublishBotID = ""
	case "bot":
		if req.PublishBotID == "" {
			app.FailWithMsg("发布 Bot 不能为空", c)
			return
		}
		if bot, ok := global.BotStore.Get(req.PublishBotID); !ok {
			app.FailWithMsg("发布 Bot 不存在", c)
			return
		} else if bot.Disabled {
			app.FailWithMsg("发布 Bot 已禁用", c)
			return
		}
		req.PublishSessionKey = ""
	default:
		app.FailWithMsg("发布类型不支持", c)
		return
	}

	strategy, err := service.GetStrategyByID(c.Request.Context(), userID, req.StrategyID)
	if err != nil {
		app.FailWithMsg("策略不存在或无权操作: "+err.Error(), c)
		return
	}
	types := strategy.AllowedTypes
	if len(types.Strings()) == 0 {
		types = strategy.ContentTypes
	}
	blockExts := strategy.BlockFileExts
	allowExts := strategy.AllowFileExts

	if req.KeywordProfileID != 0 {
		if _, err := service.GetKeywordProfileByID(c.Request.Context(), userID, req.KeywordProfileID); err != nil {
			app.FailWithMsg("关键词策略不存在或无权操作: "+err.Error(), c)
			return
		}
	}

	task := model.Task{
		UserID:            userID,
		SourceURL:         req.SourceURL,
		TargetURL:         req.TargetURL,
		Remark:            req.Remark,
		ExecuteBy:         req.SessionKey,
		PublishType:       req.PublishType,
		PublishSessionKey: req.PublishSessionKey,
		PublishBotID:      req.PublishBotID,
		StrategyID:        req.StrategyID,
		KeywordProfileID:  req.KeywordProfileID,

		CloneMode:     strategy.CloneMode,
		ContentTypes:  types,
		BlockFileExts: blockExts,
		AllowFileExts: allowExts,

		ScopeType:  strategy.ScopeType,
		ScopeValue: strings.TrimSpace(strategy.ScopeValue),

		KeepReply:       strategy.KeepReply,
		Realtime:        strategy.EnableRealtime || strategy.Realtime,
		CloneComment:    strategy.CloneComment,
		GpuAccel:        strategy.GpuAccel,
		ChangeMD5:       strategy.ChangeMD5,
		RandomFilename:  strategy.ChangeMD5 && strategy.RandomFilename,
		EnableMediaEdit: strategy.EnableMediaEdit,

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
	if task.PublishType != "" {
		// Separated publish requires download+upload.
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

	if err := service.CreateTask(c.Request.Context(), &task); err != nil {
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

	list, err := service.GetTaskList(c.Request.Context(), userID)
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
	if global.Logger != nil {
		global.Logger.Info("task action requested", zap.Uint("task_id", req.ID), zap.Uint("user_id", userID), zap.String("action", req.Action))
	}

	task, err := service.ApplyTaskAction(c.Request.Context(), userID, req.ID, req.Action)
	if err != nil {
		if global.Logger != nil {
			global.Logger.Warn("task action rejected", zap.Uint("task_id", req.ID), zap.Uint("user_id", userID), zap.String("action", req.Action), zap.Error(err))
		}
		app.FailWithMsg("状态更新失败: "+err.Error(), c)
		return
	}
	if global.Logger != nil {
		global.Logger.Info("task action accepted", zap.Uint("task_id", task.ID), zap.Uint("user_id", userID), zap.String("action", req.Action), zap.Int("status", task.Status))
	}

	// Runtime side-effects are handled in API layer to keep service package DB-only
	// (prevents service -> engine dependency and potential import cycles).
	switch req.Action {
	case "start":
		engine.Manager.StartTask(task)
	case "restart":
		engine.Manager.RestartTask(task)
	case "pause":
		engine.Manager.PauseTask(task.ID)
	case "stop":
		engine.Manager.StopTask(task.ID)
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

	task, err := service.GetTaskByID(c.Request.Context(), userID, uint(idU64))
	if err != nil {
		app.FailWithMsg("任务不存在或无权操作: "+err.Error(), c)
		return
	}

	progress := engine.Manager.GetTaskProgress(task)
	app.OkWithData(progress, c)
}

// GetTaskProgressBatch returns progress for multiple tasks in a single request to avoid N+1 polling.
// GET /api/v1/tasks/progress?ids=1,2,3
func (a *TaskApi) GetTaskProgressBatch(c *gin.Context) {
	userID := getCurrentUserID(c)
	if userID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
	}
	raw := strings.TrimSpace(c.Query("ids"))
	if raw == "" {
		app.OkWithData(map[uint]engine.TaskProgress{}, c)
		return
	}

	parts := strings.Split(raw, ",")
	ids := make([]uint, 0, len(parts))
	seen := make(map[uint]struct{}, len(parts))
	for _, p := range parts {
		if len(ids) >= 200 {
			break
		}
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		u64, err := strconv.ParseUint(p, 10, 64)
		if err != nil || u64 == 0 {
			continue
		}
		id := uint(u64)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		app.OkWithData(map[uint]engine.TaskProgress{}, c)
		return
	}
	if global.DB == nil {
		app.FailWithMsg("数据库未初始化", c)
		return
	}

	var tasks []model.Task
	if err := global.DB.WithContext(c.Request.Context()).Where("user_id = ? AND id IN ?", userID, ids).Find(&tasks).Error; err != nil {
		app.FailWithMsg("查询任务失败: "+err.Error(), c)
		return
	}

	out := make(map[uint]engine.TaskProgress, len(tasks))
	for _, t := range tasks {
		if t.ID == 0 {
			continue
		}
		out[t.ID] = engine.Manager.GetTaskProgress(t)
	}
	app.OkWithData(out, c)
}

// UpdateTask 更新任务基础信息与策略引用（更新后状态重置为停止）
func (a *TaskApi) UpdateTask(c *gin.Context) {
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

	var req UpdateTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		app.FailWithMsg("配置参数格式错误: "+err.Error(), c)
		return
	}

	req.SourceURL = strings.TrimSpace(req.SourceURL)
	req.TargetURL = strings.TrimSpace(req.TargetURL)
	req.SessionKey = strings.TrimSpace(req.SessionKey)
	req.Remark = strings.TrimSpace(req.Remark)
	req.PublishType = strings.TrimSpace(req.PublishType)
	req.PublishSessionKey = strings.TrimSpace(req.PublishSessionKey)
	req.PublishBotID = strings.TrimSpace(req.PublishBotID)

	if req.SourceURL == "" || req.TargetURL == "" || req.SessionKey == "" || req.StrategyID == 0 {
		app.FailWithMsg("更新参数不完整", c)
		return
	}

	if req.PublishType == "same" {
		req.PublishType = ""
	}
	switch req.PublishType {
	case "":
		req.PublishSessionKey = ""
		req.PublishBotID = ""
	case "account":
		if req.PublishSessionKey == "" {
			app.FailWithMsg("发布账号不能为空", c)
			return
		}
		if req.PublishSessionKey == req.SessionKey {
			req.PublishType = ""
			req.PublishSessionKey = ""
			req.PublishBotID = ""
		}
		req.PublishBotID = ""
	case "bot":
		if req.PublishBotID == "" {
			app.FailWithMsg("发布 Bot 不能为空", c)
			return
		}
		if bot, ok := global.BotStore.Get(req.PublishBotID); !ok {
			app.FailWithMsg("发布 Bot 不存在", c)
			return
		} else if bot.Disabled {
			app.FailWithMsg("发布 Bot 已禁用", c)
			return
		}
		req.PublishSessionKey = ""
	default:
		app.FailWithMsg("发布类型不支持", c)
		return
	}

	strategy, err := service.GetStrategyByID(c.Request.Context(), userID, req.StrategyID)
	if err != nil {
		app.FailWithMsg("策略不存在或无权操作: "+err.Error(), c)
		return
	}
	types := strategy.AllowedTypes
	if len(types.Strings()) == 0 {
		types = strategy.ContentTypes
	}
	blockExts := strategy.BlockFileExts
	allowExts := strategy.AllowFileExts

	if req.KeywordProfileID != 0 {
		if _, err := service.GetKeywordProfileByID(c.Request.Context(), userID, req.KeywordProfileID); err != nil {
			app.FailWithMsg("关键词策略不存在或无权操作: "+err.Error(), c)
			return
		}
	}

	payload := model.Task{
		SourceURL:         req.SourceURL,
		TargetURL:         req.TargetURL,
		Remark:            req.Remark,
		ExecuteBy:         req.SessionKey,
		PublishType:       req.PublishType,
		PublishSessionKey: req.PublishSessionKey,
		PublishBotID:      req.PublishBotID,
		StrategyID:        req.StrategyID,
		KeywordProfileID:  req.KeywordProfileID,

		CloneMode:     strategy.CloneMode,
		ContentTypes:  types,
		BlockFileExts: blockExts,
		AllowFileExts: allowExts,

		ScopeType:  strategy.ScopeType,
		ScopeValue: strings.TrimSpace(strategy.ScopeValue),

		KeepReply:       strategy.KeepReply,
		Realtime:        strategy.EnableRealtime || strategy.Realtime,
		CloneComment:    strategy.CloneComment,
		GpuAccel:        strategy.GpuAccel,
		ChangeMD5:       strategy.ChangeMD5,
		RandomFilename:  strategy.ChangeMD5 && strategy.RandomFilename,
		EnableMediaEdit: strategy.EnableMediaEdit,

		DelayMinMs: strategy.DelayMinMs,
		DelayMaxMs: strategy.DelayMaxMs,

		DailyLimit: strategy.DailyLimit,
		RunWindow:  strings.TrimSpace(strategy.RunWindow),

		Status: model.TaskStatusStopped,

		HistoryOrder: strategy.HistoryOrder,
	}

	if payload.CloneMode == 0 {
		payload.CloneMode = 3
	}
	if payload.PublishType != "" {
		payload.CloneMode = 3
	}
	if payload.ScopeType == 0 {
		payload.ScopeType = 1
	}
	if payload.HistoryOrder == 0 {
		payload.HistoryOrder = 1
	}
	if payload.DelayMinMs < 0 {
		payload.DelayMinMs = 0
	}
	if payload.DelayMaxMs < payload.DelayMinMs {
		payload.DelayMaxMs = payload.DelayMinMs
	}
	if payload.DailyLimit < 0 {
		payload.DailyLimit = 0
	}

	// Ensure the worker is stopped before applying changes.
	engine.Manager.StopTask(uint(idU64))

	updated, err := service.UpdateTask(c.Request.Context(), userID, uint(idU64), &payload)
	if err != nil {
		app.FailWithMsg("任务更新失败: "+err.Error(), c)
		return
	}
	app.OkWithData(updated, c)
}

// DeleteTask 删除任务（删除前确保后台 Worker 已停止）
func (a *TaskApi) DeleteTask(c *gin.Context) {
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

	taskID := uint(idU64)
	engine.Manager.StopTask(taskID)
	if err := engine.Manager.DestroyLocalDB(taskID); err != nil && global.Logger != nil {
		global.Logger.Warn("destroy task localdb failed: " + err.Error())
	}

	if err := service.DeleteTask(c.Request.Context(), userID, uint(idU64)); err != nil {
		app.FailWithMsg("任务删除失败: "+err.Error(), c)
		return
	}
	app.OkWithData(gin.H{"ok": true}, c)
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

	return 0
}
