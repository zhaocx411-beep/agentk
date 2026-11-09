// Package artifact 提供产物字节存储及其访问属性：
// 图片、视频等二进制内容的写入与按 URL 回读，不涉及任何数据库登记。
package artifact

import (
	"context"
	"io"
)

// Visibility 描述产物 URL 对进程外公网服务的可达性。
type Visibility uint8

const (
	// Private 表示 URL 只在本机或内网可达，交给公网服务前需要读取并内联。
	Private Visibility = iota
	// Public 表示 URL 可由公网服务直接读取。
	Public
)

// Object 是 Storage 中一个对象的稳定定位信息。
type Object struct {
	Key        string
	URL        string
	Visibility Visibility
}

// Storage 是产物二进制存储。实现必须能识别并读取自己生成的 URL。
type Storage interface {
	Put(ctx context.Context, key string, data []byte, contentType string) (Object, error)
	Resolve(uri string) (Object, bool)
	Open(ctx context.Context, object Object) (io.ReadCloser, error)
}
