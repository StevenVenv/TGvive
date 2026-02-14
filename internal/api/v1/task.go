package v1

import (
	"strconv"

	"my-go-server/internal/model"
	"my-go-server/internal/service"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TaskApi struct{}

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

	// 开发期兜底：没有 JWT 的情况下默认为 1
	return 1
}
