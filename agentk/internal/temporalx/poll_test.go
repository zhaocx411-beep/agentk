package temporalx

import (
	"testing"
	"time"
)

// 轮询退避随任务年龄拉长，抖动限制在 ±10% 内。
func TestJobPollDelayBackoff(t *testing.T) {
	for _, c := range []struct {
		age  time.Duration
		want time.Duration
	}{
		{time.Second, 2 * time.Second},
		{time.Minute, 5 * time.Second},
		{5 * time.Minute, 15 * time.Second},
		{time.Hour, 30 * time.Second},
	} {
		for range 20 {
			got := JobPollDelay(c.age, DefaultJobPollBackoff)
			low := time.Duration(float64(c.want) * 0.9)
			high := time.Duration(float64(c.want) * 1.1)
			if got < low || got > high {
				t.Fatalf("delay at age %v = %v, want within ±10%% of %v", c.age, got, c.want)
			}
		}
	}
}
