package v1

import (
	"my-go-server/internal/service"
	"my-go-server/pkg/app"

	"github.com/gin-gonic/gin"
)

func Ping(c *gin.Context) {
	app.OK(c, service.Ping())
}
