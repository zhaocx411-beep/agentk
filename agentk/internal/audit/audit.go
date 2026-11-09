// Package audit 输出资源写操作的审计记录：谁（principal）对哪个资源做了什么。
// V1 输出是进程日志里的单行 JSON（前缀 "audit "），日志采集侧按前缀提取即可；
// 需要独立审计表 / 合规存档时升级实现，调用面不变。
package audit

import (
	"context"
	"encoding/json"
	"log"

	"agentk/internal/authz"
)

// Record 记一条审计。action 用"资源.动作"命名（session.create / run.create ...）；
// resource 是资源的 public id。身份取自 context 的 principal（缺失时只记动作，
// 不阻塞业务路径——写路径本身已被身份中间件保护）。
func Record(ctx context.Context, action, resource string) {
	write(entryFor(ctx, action, resource))
}

func entryFor(ctx context.Context, action, resource string) auditEntry {
	entry := auditEntry{Action: action, Resource: resource}
	if p, ok := authz.FromContext(ctx); ok {
		entry.Tenant = p.TenantID
		entry.ActorKind = actorKindService
		entry.ServicePrincipal = p.UserPub
	}
	return entry
}

// RecordOperator 记录 agentk-admin 的带外管理操作。
func RecordOperator(action, resource string) {
	write(auditEntry{Action: action, Resource: resource, ActorKind: actorKindOperator})
}

// ActorKind 区分两类记录来源：service 是走 API 的应用接入方，
// operator 是 agentk-admin 的带外管理操作。
const (
	actorKindService  = "service"
	actorKindOperator = "operator"
)

type auditEntry struct {
	Action           string `json:"action"`
	Resource         string `json:"resource"`
	Tenant           string `json:"tenant,omitempty"`
	ServicePrincipal string `json:"servicePrincipal,omitempty"`
	ActorKind        string `json:"actorKind,omitempty"`
}

func write(entry auditEntry) {
	raw, err := json.Marshal(entry)
	if err != nil {
		log.Printf("audit: marshal %s %s: %v", entry.Action, entry.Resource, err)
		return
	}
	log.Printf("audit %s", raw)
}
