package engine

import (
	"strings"

	"github.com/gotd/td/tgerr"
)

func isTGError(err error, code string) bool {
	if err == nil {
		return false
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return false
	}
	if tgerr.Is(err, code) {
		return true
	}
	return strings.Contains(strings.ToUpper(err.Error()), code)
}

func isTGMsgIDInvalid(err error) bool {
	return isTGError(err, "MSG_ID_INVALID")
}
