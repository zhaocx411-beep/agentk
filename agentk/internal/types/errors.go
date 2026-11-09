package types

import "errors"

// ErrNotFound 是跨存储实现的查询未命中哨兵（API 层译为 404）。
// 定义在本包：MySQL 实现与内存实现共用，消费方判错不绑定具体实现。
var (
	ErrNotFound        = errors.New("not found")
	ErrExecutionActive = errors.New("session execution is active")
	ErrExecutionIdle   = errors.New("session execution is not active")
)
