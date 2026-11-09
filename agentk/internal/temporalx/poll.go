package temporalx

import (
	"math/rand/v2"
	"time"
)

// DefaultJobPollBackoff 是异步任务的缺省轮询退避：新任务查得勤，老任务查得稀。
// 视频生成动辄几分钟，前 30 秒每 2 秒一次让"刚提交就完成"的短任务尽快回来，
// 之后拉长到分钟级。
var DefaultJobPollBackoff = []JobPollStep{
	{Age: 30 * time.Second, Delay: 2 * time.Second},
	{Age: 2 * time.Minute, Delay: 5 * time.Second},
	{Age: 10 * time.Minute, Delay: 15 * time.Second},
	{Age: 0, Delay: 30 * time.Second},
}

// JobPollStep 是一档轮询间隔：任务年龄小于 Age 时用 Delay；Age 为 0 表示兜底档。
type JobPollStep struct {
	Age   time.Duration
	Delay time.Duration
}

// JobPollDelay 按任务年龄给出下次轮询间隔，带 ±10% 抖动避免同批任务共振。
func JobPollDelay(age time.Duration, steps []JobPollStep) time.Duration {
	delay := 30 * time.Second
	for _, s := range steps {
		if s.Age == 0 || age < s.Age {
			delay = s.Delay
			break
		}
	}
	jitter := 1 + (rand.Float64()-0.5)/5
	return time.Duration(float64(delay) * jitter)
}
