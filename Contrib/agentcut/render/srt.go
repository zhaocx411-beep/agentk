package render

import (
	"encoding/base64"
	"fmt"
	"strings"

	"agentcut/timeline"
)

// BuildSRT 把 text 片段编译成 base64 编码的 SRT 内容（burn_subtitles 的
// subtitle 槽位接受 base64:xxx 内联）。调用方保证 items 已排序。
func BuildSRT(items []timeline.Item, fps int) string {
	var b strings.Builder
	n := 0
	for _, it := range items {
		if it.Kind != timeline.ItemText || strings.TrimSpace(it.Text) == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n",
			n,
			srtTimestamp(it.From, fps),
			srtTimestamp(it.End(), fps),
			strings.TrimSpace(it.Text),
		)
	}
	return base64.StdEncoding.EncodeToString([]byte(b.String()))
}

// srtTimestamp 把帧转成 SRT 时间戳（HH:MM:SS,mmm）。
func srtTimestamp(frames int64, fps int) string {
	ms := (frames*1000 + int64(fps)/2) / int64(fps)
	h := ms / 3600000
	m := ms % 3600000 / 60000
	s := ms % 60000 / 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms%1000)
}
