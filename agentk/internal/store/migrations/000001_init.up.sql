-- AgentK 当前完整 schema。空库由本文件创建；KDB 环境的结构变更带外执行，
-- 执行后必须与本文件保持一致。
--
-- 模型是两层：会话 + 事件日志。事件流是唯一真相源，喂给模型的对话历史由事件
-- fold 投影而来，不存在与之并列的 item 实体；执行状态归 Temporal，DB 只存事件。
--
-- 纪律：全部显式列；载荷 JSON 整存整取（格式与推送一致，由 spec/fixtures/events
-- 钉死）；保守 MySQL 8.0 公共子集；不用外键约束（自建库/分库友好），一致性由
-- 应用层事务保证。表前缀 ak_ 为占位。
--
-- DDL 形态适配公司 DB 平台工单 lint：每列必须 COMMENT、表必须显式 COLLATE、
-- 唯一索引必须 uniq_ 前缀（kdb 部署走工单执行本文件，见 deploy/kdb/）。
--
-- 业务表的 tenant_id 取值 = ak_orgs.public_id；外部 IdP 的 subject 只存在于
-- ak_identities，换账户体系 = 加 provider + account linking，业务数据零迁移。
-- ak_events 不带 tenant_id：它没有 public_id 直查入口，隔离在会话解析处强制。

CREATE TABLE ak_sessions (
    id                  BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '内部主键，仅进程内使用，不对外',
    public_id           VARCHAR(32)     NOT NULL COMMENT '对外 id（ses_ 前缀随机串）',
    tenant_id           VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '租户，取值 = ak_orgs.public_id',
    title               VARCHAR(256)    NOT NULL DEFAULT '' COMMENT '会话标题',
    profile             JSON            NOT NULL COMMENT '创建时固定的有效 Profile（规范 JSON，已校验且不含凭证）',
    created_at          DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    updated_at          DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
    PRIMARY KEY (id),
    UNIQUE KEY uniq_sessions_public_id (public_id),
    KEY idx_sessions_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='会话（事件流的宿主）';

-- 事件日志：只插不改不删。
--
-- 自增 id 只负责内部排序；event_id 是对外稳定身份。batch 两列承担 Activity
-- 重试的写入幂等：同一逻辑批次重试时读回首次提交结果。
CREATE TABLE ak_events (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '内部主键，仅用于排序',
    event_id     VARCHAR(64)     NOT NULL COMMENT '对外事件 id（evt_ 前缀 UUIDv7）',
    session_id   BIGINT UNSIGNED NOT NULL COMMENT '所属会话（ak_sessions.id）',
    execution_id VARCHAR(64)     NOT NULL COMMENT '所属 Execution（exe_ 前缀 UUIDv7）',
    round_id     VARCHAR(32)     NOT NULL DEFAULT '' COMMENT '所属模型轮次；空串表示轮外事件',
    batch_id     VARCHAR(64)     NOT NULL COMMENT '幂等写入批标识；Activity 重试复用同一值',
    batch_idx    INT UNSIGNED    NOT NULL COMMENT '批内下标（0..n）',
    kind         VARCHAR(64)     NOT NULL COMMENT '事件类型（见 internal/event 的 Kind）',
    payload      JSON            NOT NULL COMMENT '事件载荷，整存整取',
    created_at   DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    PRIMARY KEY (id),
    UNIQUE KEY uniq_events_event_id (event_id),
    UNIQUE KEY uniq_events_batch (session_id, batch_id, batch_idx),
    KEY idx_events_session_id (session_id, id),
    KEY idx_events_execution_id (execution_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='Session 的 append-only 事件日志';

-- 身份六表：内部稳定账户（users）+ 外部身份映射（identities）+ 租户（orgs）
-- + 成员关系（memberships）+ 服务主体（service_principals）+ API Key（api_keys）。
-- 消费方见 internal/authz（中间件）与 internal/store/identity.go、auth.go（实现）。

CREATE TABLE ak_users (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '内部主键',
    public_id    VARCHAR(32)     NOT NULL COMMENT '对外 id（usr_ 前缀随机串）',
    display_name VARCHAR(128)    NOT NULL DEFAULT '' COMMENT '展示名，不作任何 id',
    email        VARCHAR(256)    NOT NULL DEFAULT '' COMMENT '邮箱（展示/通知用）',
    status       VARCHAR(16)     NOT NULL DEFAULT 'active' COMMENT '账户状态：active / disabled',
    created_at   DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    PRIMARY KEY (id),
    UNIQUE KEY uniq_users_public_id (public_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='内部稳定账户';

CREATE TABLE ak_identities (
    id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '内部主键',
    user_id          BIGINT UNSIGNED NOT NULL COMMENT '归属账户（ak_users.id）',
    provider         VARCHAR(32)     NOT NULL COMMENT '身份提供方：corp-sso（内网 SSO 网关）；终端用户体系上线时加新值',
    external_subject VARCHAR(128)    NOT NULL COMMENT '外部稳定 subject；corp-sso 下 = X-Sso-User-Id（工号），坚决不用会变的域账号',
    raw_profile      JSON            NULL COMMENT '最近一次登录的原始头快照，展示/排查用，不作任何 id',
    created_at       DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    last_seen_at     DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '最近一次访问时间',
    PRIMARY KEY (id),
    UNIQUE KEY uniq_identities_provider_subject (provider, external_subject),
    KEY idx_identities_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='外部身份映射（JIT 并发开户靠唯一键兜底：插入冲突则回读）';

CREATE TABLE ak_orgs (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '内部主键',
    public_id  VARCHAR(32)     NOT NULL COMMENT '对外 id（org_ 前缀随机串），即业务表 tenant_id 的取值',
    name       VARCHAR(128)    NOT NULL DEFAULT '' COMMENT '组织名',
    kind       VARCHAR(16)     NOT NULL DEFAULT 'personal' COMMENT '类别：personal（内网一人一租户）/ team / enterprise（预留）',
    status     VARCHAR(16)     NOT NULL DEFAULT 'active' COMMENT '组织状态：active / disabled',
    created_at DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    PRIMARY KEY (id),
    UNIQUE KEY uniq_orgs_public_id (public_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='组织（租户边界）';

CREATE TABLE ak_memberships (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '内部主键',
    org_id     BIGINT UNSIGNED NOT NULL COMMENT '组织（ak_orgs.id）',
    user_id    BIGINT UNSIGNED NOT NULL COMMENT '账户（ak_users.id）',
    role       VARCHAR(16)     NOT NULL DEFAULT 'owner' COMMENT '角色：owner / member（预留，本期恒 owner；应用域 RBAC 归上层 app）',
    created_at DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    PRIMARY KEY (id),
    UNIQUE KEY uniq_memberships_org_user (org_id, user_id),
    KEY idx_memberships_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='用户-组织成员关系';

-- 服务身份：下游应用以绑定 org 的 Service Principal 调用 /v1。
-- API Key 只保存 SHA-256 摘要，明文仅在创建时展示一次。

CREATE TABLE ak_service_principals (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '内部主键',
    public_id  VARCHAR(32)     NOT NULL COMMENT '对外 id（sp_ 前缀随机串）',
    org_id     BIGINT UNSIGNED NOT NULL COMMENT '归属组织（ak_orgs.id）',
    tenant_id  VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '租户冗余，取值 = ak_orgs.public_id',
    name       VARCHAR(128)    NOT NULL DEFAULT '' COMMENT '服务主体展示名',
    status     VARCHAR(16)     NOT NULL DEFAULT 'active' COMMENT '状态：active / disabled',
    created_at DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    updated_at DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
    PRIMARY KEY (id),
    UNIQUE KEY uniq_service_principals_public_id (public_id),
    KEY idx_service_principals_org (org_id),
    KEY idx_service_principals_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='服务主体（机器身份，绑定租户）';

CREATE TABLE ak_api_keys (
    id                   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '内部主键',
    public_id            VARCHAR(32)     NOT NULL COMMENT '对外 credential id（key_ 前缀，非 secret）',
    service_principal_id BIGINT UNSIGNED NOT NULL COMMENT '归属服务主体（ak_service_principals.id）',
    key_prefix           VARCHAR(16)     NOT NULL COMMENT 'Bearer token 中的查找前缀',
    secret_hash          VARBINARY(32)   NOT NULL COMMENT '高熵 API Key 的 SHA-256 摘要',
    status               VARCHAR(16)     NOT NULL DEFAULT 'active' COMMENT '状态：active / revoked',
    expires_at           DATETIME(3)     NULL COMMENT '过期时间；NULL 表示永不过期',
    last_used_at         DATETIME(3)     NULL COMMENT '最近一次成功鉴权时间',
    revoked_at           DATETIME(3)     NULL COMMENT '吊销时间',
    created_at           DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    PRIMARY KEY (id),
    UNIQUE KEY uniq_api_keys_public_id (public_id),
    UNIQUE KEY uniq_api_keys_prefix (key_prefix),
    KEY idx_api_keys_principal (service_principal_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='Service Principal API Key（明文不落库）';
