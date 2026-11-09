// Package capability 包含 AgentK 的内置能力。
// 每个子包拥有自己的领域输入输出、provider 适配和执行体；执行体可以是普通函数、
// Temporal Activity，或组合其他能力的 Temporal Workflow。
// tool.Spec、pipeline 声明和 Temporal ABI 是同一能力的不同调用入口。
// tool、pipeline、agentloop 等机制层不属于本包，业务能力编排属于本包。
package capability
