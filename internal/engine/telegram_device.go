package engine

import (
	"runtime"
	"strings"

	"github.com/gotd/td/telegram"
)

func humanGOOS(goos string) string {
	goos = strings.ToLower(strings.TrimSpace(goos))
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "":
		return "Unknown"
	default:
		return strings.ToUpper(goos[:1]) + goos[1:]
	}
}

// defaultDeviceConfig overrides gotd defaults so Telegram "Active Sessions" shows a human-friendly
// device/app name instead of runtime.Version() like "go1.xx".
func defaultDeviceConfig() telegram.DeviceConfig {
	osName := humanGOOS(runtime.GOOS)
	return telegram.DeviceConfig{
		DeviceModel:   osName,
		SystemVersion: osName,
		AppVersion:    "1.0",
		// Keep language defaults ("en") by leaving SystemLangCode/LangCode empty.
	}
}
