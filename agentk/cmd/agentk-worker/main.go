// agentk-worker 独立运行 AgentK 的 Temporal Workflow 和 Activity。
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"syscall"

	"agentk/internal/app"
)

func main() {
	// RunWorker 返回前执行所有 defer，再由 main 转换为非零退出状态。
	if err := app.RunWorker(notifyReady); err != nil {
		log.Fatal(err)
	}
}

// notifyReady 在 AGENTK_READY_SIGNAL_PID 已设置时向该进程发送 SIGUSR1。
func notifyReady() error {
	rawPID := os.Getenv("AGENTK_READY_SIGNAL_PID")
	if rawPID == "" {
		return nil
	}
	pid, err := strconv.Atoi(rawPID)
	if err != nil || pid <= 0 {
		return fmt.Errorf("invalid AGENTK_READY_SIGNAL_PID %q", rawPID)
	}
	if err := syscall.Kill(pid, syscall.SIGUSR1); err != nil {
		return fmt.Errorf("signal ready to pid %d: %w", pid, err)
	}
	return nil
}
