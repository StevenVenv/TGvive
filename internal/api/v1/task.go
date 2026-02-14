package v1

import (
	"strconv"

	"my-go-server/internal/engine"
	"my-go-server/internal/model"
	"my-go-server/internal/service"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TaskApi struct{}

// TaskActionReq 任务操作请求
type TaskActionReq struct {
	ID     uint   `json:"id" binding:"required"`
	Action string `json:"action" binding:"required,oneof=start stop pause"`
}

// CreateTask 创建搬运任务
func (a *TaskApi) CreateTask(c *gin.Context) {
	var task model.Task
	if err := c.ShouldBindJSON(&task); err != nil {
		app.FailWithMsg("配置参数格式错误: "+err.Error(), c)
		return
	}

	task.Model = gorm.Model{}
	task.UserID = getCurrentUserID(c)
	if task.UserID == 0 {
		app.FailWithMsg("未获取到用户信息", c)
		return
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

	return 0
}
