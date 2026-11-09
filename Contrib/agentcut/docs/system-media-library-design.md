# 系统素材库设计草案

状态：待产品/后端确认，本文不包含实现。

## 目标与边界

系统素材是平台维护、可搜索、可被多个项目复用的正版视频、图片、BGM 和音效。它和用户上传素材分属不同生命周期：用户删除项目不能删除系统素材，系统素材下架也不能破坏已经生成的项目和成片。

## 推荐存储

1. 二进制继续复用现有 Blob Storage，但使用独立的 `catalog/` key 前缀和独立清理策略；项目的 `DeleteMediaBlob` Activity 永远不能删除该前缀。
2. 元数据使用不可变、带版本的 JSON manifest 存入 Blob，例如 `catalog/manifests/{version}.json`。
3. 新建轻量 `CatalogRegistryWorkflow`，只保存当前 manifest 版本、URI 和发布时间，不在 Workflow History 中保存整个目录。`cut-apiserver` 按版本加载并缓存 manifest，在内存中完成 M1 的关键词、类型、标签和时长过滤。
4. 目录达到需要全文检索、复杂版权过滤的规模后，再把查询实现替换为搜索服务；公开 HTTP 契约不变。

这样不引入新数据库，也避免把数千条可变素材写进 Temporal History。

## 数据模型

新增独立的 `CatalogAsset`，不直接复用项目内 `timeline.Media`：

- `assetId`、`kind`、`displayName`、`description`
- `blobKey`、`contentType`、`sizeBytes`、`durationMs`
- `width`、`height`、`fps`、`waveform/thumbnail`
- `tags`、`locale`、`licenseId`、`licenseVersion`
- `status`（active/deprecated/blocked）、`publishedAt`、`updatedAt`

项目添加素材时生成项目局部的 `mediaId`，并把下列快照写入 `timeline.Media`：

- 新增 `origin: "upload" | "catalog" | "generated"`
- 新增 `catalogAssetId`、`catalogVersion`、`licenseSnapshot`（均可选）
- `URI` 指向受控的 catalog content 接口
- `BlobKey` 留空，避免现有项目级 GC 误删共享对象

项目继续引用固定的目录版本；素材下架只阻止新项目添加，已引用项目保留只读访问。法律要求强制下架时由独立策略覆盖。

## 建议接口

- `GET /v1/catalog/assets?q=&kind=&tags=&pageToken=&limit=`
- `GET /v1/catalog/assets/{assetId}`
- `GET /v1/catalog/assets/{assetId}/content`
- `POST /v1/projects/{projectId}/catalog-assets/{assetId}:attach`
  - 校验用户、素材状态和许可证
  - 在项目中创建一个 `timeline.Media` 引用
  - 返回 `{media, revision}`；不自动创建 Timeline item

发布管理接口仅开放给平台后台，不和普通用户 API 共用权限。

## 需要确认

- 版权是否允许“目录版本下架后，旧项目继续访问”
- 是否需要按地区、租户和套餐过滤
- 素材添加后是共享引用还是复制进项目；建议默认共享引用，仅在许可证要求时复制
- 渲染 Worker 访问 catalog content 的服务身份和签名 URL 时效
