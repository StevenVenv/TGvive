package app

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"my-go-server/pkg/e"
)

type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

func JSON(c *gin.Context, code int, msg string, data any) {
	c.JSON(http.StatusOK, Response{
		Code: code,
		Msg:  msg,
		Data: data,
	})
}

func OK(c *gin.Context, data any) {
	JSON(c, e.CodeSuccess, "ok", data)
}

func Fail(c *gin.Context, code int, msg string) {
	JSON(c, code, msg, nil)
}

func OkWithData(data any, c *gin.Context) {
	OK(c, data)
}

func FailWithMsg(msg string, c *gin.Context) {
	Fail(c, e.CodeError, msg)
}
