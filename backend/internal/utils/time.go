package utils

import (
	"fmt"
	"time"
)

func NowStr() string {
	return time.Now().Format(time.RFC3339)
}

func FormatUnixMilliForLog(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

func FormatBrowserCloseEventDetail(prefix string, closedAtMS, timeoutAtMS int64, idleTTLSeconds int) string {
	closedAt := FormatUnixMilliForLog(closedAtMS)
	timeoutAt := FormatUnixMilliForLog(timeoutAtMS)
	return fmt.Sprintf("%s，开始触发时间：%s，超时时长：%d 秒，超时时间：%s", prefix, closedAt, idleTTLSeconds, timeoutAt)
}

func FormatBrowserCloseCancelDetail(closedAtMS, timeoutAtMS, reopenedAtMS int64, idleTTLSeconds int) string {
	closedAt := FormatUnixMilliForLog(closedAtMS)
	timeoutAt := FormatUnixMilliForLog(timeoutAtMS)
	reopenedAt := FormatUnixMilliForLog(reopenedAtMS)
	return fmt.Sprintf("浏览器系统页面已重新打开，取消页面关闭超时计时，开始触发时间：%s，超时时长：%d 秒，原超时时间：%s，重新打开时间：%s", closedAt, idleTTLSeconds, timeoutAt, reopenedAt)
}
