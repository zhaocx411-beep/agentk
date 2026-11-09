# AgentK Upstream

供 AgentK 和 contrib worker 使用的上游客户端：模型网关、对话协议适配及 Keffects workflow 绑定。

## 接入

- Kigress、Klink 自动选择入口：`KWS_SERVICE_STAGE` 为空或 `STAGING` 时使用 staging 地址，其余使用 IDC 地址。
  调用方提供租户信息和凭据。
- Keffects 默认通过 KESS 发现 `ytech-keffects-service`；其他服务名通过 `Config.ServiceName` 指定。
- Keffects 租户与 workflow 可按 name 或 ID 指定。这里的 workflow 指 Keffects 服务端任务。
- `ErrInvalidInput` 表示输入不合法；`ErrOutputMissing` 表示成功响应的输出尚不可见，可继续轮询。
- Keffects 媒体先通过 `StageMedia` 放入工作桶，成功输出复制到业务桶后由 `OutputURI` 返回业务 URI。
  调用方实现 `MediaStore`，负责对象读写、桶权限与复制；两桶相同时跳过复制。

## 同步 protobuf

将上游 proto 原样同步至 `protocol/<platform>/`，提交说明记录来源仓库及 commit / tag。
安装 `buf`、`protoc-gen-go`、`protoc-gen-go-grpc` 后，在仓库根目录执行：

```bash
buf build && buf generate
```

`go/gen/` 会清空重建，proto 与生成代码一起提交。
