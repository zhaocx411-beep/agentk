// agentk-api 启动 HTTP API；standalone 模式同时启动 Temporal Worker。
package main

import (
	"log"

	"agentk/internal/app"
)

func main() {
	// RunAPI 返回前执行所有 defer，避免直接 Fatal 跳过 Worker 和 client 关闭。
	if err := app.RunAPI(); err != nil {
		log.Fatal(err)
	}
}
