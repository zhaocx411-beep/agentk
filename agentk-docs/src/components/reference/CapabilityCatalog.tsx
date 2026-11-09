import {
  Component,
  useEffect,
  useState,
  type ErrorInfo,
  type ReactNode,
} from 'react';
import useBaseUrl from '@docusaurus/useBaseUrl';
import styles from './CapabilityCatalog.module.css';

type JsonSchema = unknown;

type CapabilityTool = {
  description: string;
  maxAttempts?: number;
};

type Capability = {
  id: string;
  source: 'core' | 'contrib' | 'unknown';
  description: string;
  inputSchema: JsonSchema;
  outputSchema: JsonSchema;
  timeoutSeconds: number;
  lastReportedAt?: string;
  tool?: CapabilityTool;
};

type Freshness = {
  label: string;
  detail: string;
  summary: string;
  tone: 'core' | 'fresh' | 'delayed' | 'stale';
  reportedAt?: string;
};

const POLL_INTERVAL_MS = 60_000;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function optionalString(value: unknown): string | undefined {
  return typeof value === 'string' && value.length > 0 ? value : undefined;
}

function normalizeTool(value: unknown): CapabilityTool | undefined {
  if (!isRecord(value)) return undefined;
  return {
    description: optionalString(value.description) ?? '',
    maxAttempts:
      typeof value.maxAttempts === 'number' &&
      Number.isInteger(value.maxAttempts) &&
      value.maxAttempts > 0
        ? value.maxAttempts
        : undefined,
  };
}

function normalizeSource(value: unknown): Capability['source'] {
  if (value === 'CAPABILITY_SOURCE_CORE') return 'core';
  if (value === 'CAPABILITY_SOURCE_CONTRIB') return 'contrib';
  return 'unknown';
}

function normalizeDuration(value: unknown): number {
  if (typeof value !== 'string' || !/^\d+(?:\.\d+)?s$/.test(value)) {
    return 0;
  }
  return Number(value.slice(0, -1));
}

function normalizeCapability(value: unknown, index: number): Capability {
  const raw = isRecord(value) ? value : {};
  const tool = normalizeTool(raw.tool);

  return {
    id: optionalString(raw.id) ?? `invalid-capability-${index + 1}`,
    source: normalizeSource(raw.source),
    description: optionalString(raw.description) ?? '无能力说明',
    inputSchema: raw.inputSchema,
    outputSchema: raw.outputSchema,
    timeoutSeconds: normalizeDuration(raw.timeout),
    lastReportedAt: optionalString(raw.lastReportedAt),
    tool,
  };
}

function relativeTime(date: Date, now: number): string {
  const seconds = Math.max(0, Math.round((now - date.getTime()) / 1000));
  if (seconds < 60) return `${seconds} 秒前`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟前`;
  return `${Math.round(minutes / 60)} 小时前`;
}

function freshnessOf(capability: Capability, now: number): Freshness {
  if (capability.source === 'core') {
    return {
      label: '内置',
      detail: '随 AgentK 服务发布',
      summary: 'BUILT-IN',
      tone: 'core',
    };
  }
  const raw = capability.lastReportedAt;
  if (capability.source !== 'contrib' || raw === undefined) {
    return {
      label: '未知',
      detail: '上报时间未知',
      summary: 'REPORT UNKNOWN',
      tone: 'stale',
    };
  }
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) {
    return {
      label: '未知',
      detail: '上报时间无效',
      summary: 'REPORT UNKNOWN',
      tone: 'stale',
    };
  }
  const ageMinutes = (now - date.getTime()) / 60_000;
  const summary = relativeTime(date, now);
  const detail = `最近上报于 ${summary}`;
  if (ageMinutes > 30) {
    return {label: '陈旧', detail, summary, tone: 'stale', reportedAt: raw};
  }
  if (ageMinutes > 10) {
    return {label: '延迟', detail, summary, tone: 'delayed', reportedAt: raw};
  }
  return {label: '正常', detail, summary, tone: 'fresh', reportedAt: raw};
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest === 0 ? `${hours} 小时` : `${hours} 小时 ${rest} 分钟`;
}

function schemaType(schema: JsonSchema): string {
  if (schema === true) return 'any';
  if (schema === false) return 'never';
  if (!isRecord(schema)) return 'unknown';

  const type = schema.type;
  if (Array.isArray(type)) {
    const names = type.filter((item): item is string => typeof item === 'string');
    if (names.length > 0) return names.join(' | ');
  }
  if (typeof type === 'string') {
    if (type === 'array' && schema.items !== undefined) {
      return `array<${schemaType(schema.items as JsonSchema)}>`;
    }
    return type;
  }
  if (Array.isArray(schema.enum)) return 'enum';
  if (Object.prototype.hasOwnProperty.call(schema, 'const')) return 'const';
  if (typeof schema.$ref === 'string') return '$ref';
  if (isRecord(schema.properties)) return 'object';
  return 'any';
}

function schemaDescription(schema: JsonSchema): string {
  if (schema === true) return '接受任意值';
  if (schema === false) return '不接受任何值';
  if (isRecord(schema) && typeof schema.description === 'string') {
    return schema.description;
  }
  return isRecord(schema) ? '无字段说明' : '无法识别的 Schema 节点';
}

function SchemaSummary({
  label,
  schema,
}: {
  label: string;
  schema: JsonSchema;
}): ReactNode {
  const objectSchema = isRecord(schema) ? schema : undefined;
  const properties =
    objectSchema && isRecord(objectSchema.properties)
      ? (objectSchema.properties as Record<string, JsonSchema>)
      : {};
  const required = new Set(
    Array.isArray(objectSchema?.required)
      ? objectSchema.required.filter(
          (item): item is string => typeof item === 'string',
        )
      : [],
  );

  return (
    <section className={styles.schema}>
      <div className={styles.schemaTitle}>
        <strong>{label}</strong>
        <code>{schemaType(schema)}</code>
      </div>
      {Object.keys(properties).length > 0 ? (
        <div className={styles.fields}>
          {Object.entries(properties).map(([name, field]) => (
            <div className={styles.field} key={name}>
              <code>{name}</code>
              <span>{schemaType(field)}</span>
              {required.has(name) && <b>必填</b>}
              <p>{schemaDescription(field)}</p>
            </div>
          ))}
        </div>
      ) : (
        <p className={styles.emptySchema}>
          {schema === true
            ? '该契约接受任意 JSON 值。'
            : schema === false
              ? '该契约不接受任何值。'
              : '该契约没有顶层字段。'}
        </p>
      )}
      <details className={styles.rawSchema}>
        <summary>查看原始 JSON Schema</summary>
        <pre>{JSON.stringify(schema, null, 2)}</pre>
      </details>
    </section>
  );
}

class CapabilityCardBoundary extends Component<
  {capability: Capability; children: ReactNode},
  {error?: string}
> {
  state: {error?: string} = {};

  static getDerivedStateFromError(error: Error): {error: string} {
    return {error: error.message};
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error('Failed to render capability card', {
      capability: this.props.capability.id,
      error,
      info,
    });
  }

  componentDidUpdate(previous: {capability: Capability}): void {
    if (
      this.state.error &&
      previous.capability !== this.props.capability
    ) {
      this.setState({error: undefined});
    }
  }

  render(): ReactNode {
    if (this.state.error) {
      return (
        <div className={styles.error}>
          <strong>{this.props.capability.id}</strong>
          <span>
            该能力契约无法展示：{this.state.error}。其他能力不受影响。
          </span>
        </div>
      );
    }
    return this.props.children;
  }
}

function IdentityValue({
  label,
  value,
  copied,
  onCopy,
}: {
  label: string;
  value: string;
  copied: boolean;
  onCopy: () => void;
}): ReactNode {
  return (
    <div className={styles.identity}>
      <span>{label}</span>
      <code>{value}</code>
      <button type="button" onClick={onCopy}>
        {copied ? '已复制' : '复制'}
      </button>
    </div>
  );
}

function CapabilityCard({
  capability,
  now,
}: {
  capability: Capability;
  now: number;
}): ReactNode {
  const freshness = freshnessOf(capability, now);
  const badgeText =
    capability.source === 'contrib' && freshness.reportedAt
      ? `REPORTED ${freshness.summary}`
      : freshness.summary;
  const [copied, setCopied] = useState<string>();
  const [open, setOpen] = useState(false);

  async function copy(value: string) {
    await navigator.clipboard.writeText(value);
    setCopied(value);
    window.setTimeout(() => setCopied(undefined), 1500);
  }

  return (
    <details
      className={styles.card}
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary>
        <span
          className={`${styles.health} ${styles[freshness.tone]}`}
          title={`${freshness.label}：${freshness.detail}`}
        />
        <div className={styles.cardIdentity}>
          <div className={styles.identityHeading}>
            <code>{capability.id}</code>
            {freshness.reportedAt ? (
              <time
                className={`${styles.sourceBadge} ${styles[freshness.tone]}`}
                dateTime={freshness.reportedAt}
                title={freshness.detail}>
                {badgeText}
              </time>
            ) : (
              <span
                className={`${styles.sourceBadge} ${styles[freshness.tone]}`}
                title={freshness.detail}>
                {badgeText}
              </span>
            )}
          </div>
          <span className={styles.description}>{capability.description}</span>
        </div>
        <div className={styles.cardMeta}>
          {capability.tool && <span>tool</span>}
        </div>
      </summary>

      <div className={styles.cardBody}>
        <div className={styles.metadata}>
          <div>
            <span>{capability.source === 'contrib' ? 'LAST REPORT' : 'SOURCE'}</span>
            {freshness.reportedAt ? (
              <time dateTime={freshness.reportedAt} title={freshness.reportedAt}>
                {freshness.summary}
              </time>
            ) : (
              <strong>{freshness.summary}</strong>
            )}
          </div>
          <div>
            <span>TIMEOUT</span>
            <strong>{formatDuration(capability.timeoutSeconds)}</strong>
          </div>
          <div>
            <span>TOOL</span>
            <strong>
              {capability.tool
                ? capability.tool.maxAttempts
                  ? `最多 ${capability.tool.maxAttempts} 次`
                  : '已启用'
                : '未声明'}
            </strong>
          </div>
        </div>

        <div className={styles.identities}>
          <IdentityValue
            label="Capability ID"
            value={capability.id}
            copied={copied === capability.id}
            onCopy={() => void copy(capability.id)}
          />
        </div>

        <div className={styles.contracts}>
          <SchemaSummary label="输入契约" schema={capability.inputSchema} />
          <SchemaSummary label="输出契约" schema={capability.outputSchema} />
        </div>
      </div>
    </details>
  );
}

export default function CapabilityCatalog(): ReactNode {
  const endpoint = useBaseUrl('/_agentk/capabilities');
  const [capabilities, setCapabilities] = useState<Capability[]>();
  const [error, setError] = useState<string>();
  const [now, setNow] = useState(Date.now());
  const [lastSyncedAt, setLastSyncedAt] = useState<number>();
  const [refreshKey, setRefreshKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    const controller = new AbortController();

    async function load() {
      try {
        const response = await fetch(endpoint, {
          method: 'POST',
          headers: {
            Accept: 'application/json',
            'Content-Type': 'application/json',
            'Connect-Protocol-Version': '1',
          },
          body: JSON.stringify({pageSize: 100}),
          signal: controller.signal,
        });
        if (!response.ok) {
          throw new Error(
            response.status === 401
              ? 'Capability API 请求未通过认证；请检查同源 Docs Service 的 KSN 身份配置。'
              : `Capability API 返回 ${response.status}`,
          );
        }
        const payload: unknown = await response.json();
        if (!isRecord(payload) || !Array.isArray(payload.capabilities)) {
          throw new Error('Capability API 返回了无法识别的数据结构');
        }
        const nextCapabilities = payload.capabilities.map(normalizeCapability);
        if (!cancelled) {
          setCapabilities(nextCapabilities);
          setError(undefined);
          const receivedAt = Date.now();
          setNow(receivedAt);
          setLastSyncedAt(receivedAt);
        }
      } catch (cause) {
        if (!cancelled && !(cause instanceof DOMException && cause.name === 'AbortError')) {
          setError(cause instanceof Error ? cause.message : '读取 Capability API 失败');
        }
      }
    }

    void load();
    const poll = window.setInterval(() => void load(), POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      controller.abort();
      window.clearInterval(poll);
    };
  }, [endpoint, refreshKey]);

  useEffect(() => {
    const clock = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(clock);
  }, []);

  return (
    <div className={styles.catalog} data-ai-docs-ignore="true">
      <header className={styles.monitorHeader}>
        <div>
          <div className={styles.kicker}>
            <span className={styles.liveDot} />
            LIVE CATALOG
          </div>
          <h1>Capabilities</h1>
        </div>
        <div className={styles.connection}>
          <span className={error ? styles.disconnected : styles.connected}>
            {error ? 'DISCONNECTED' : capabilities ? 'CONNECTED' : 'CONNECTING'}
          </span>
          <span>{capabilities?.length ?? '—'} CAPABILITIES</span>
          <span>
            SYNC{' '}
            {lastSyncedAt
              ? new Date(lastSyncedAt).toLocaleTimeString('zh-CN', {
                  hour12: false,
                  hour: '2-digit',
                  minute: '2-digit',
                  second: '2-digit',
                })
              : '—'}
          </span>
          <span>AUTO 60S</span>
        </div>
        <button
          className={styles.refresh}
          type="button"
          onClick={() => setRefreshKey((value) => value + 1)}>
          REFRESH
        </button>
      </header>

      {error && (
        <div className={styles.error}>
          <strong>CATALOG UNAVAILABLE</strong>
          <span>{error}</span>
        </div>
      )}
      {!capabilities && !error && (
        <div className={styles.loading}>CONNECTING TO CAPABILITY API…</div>
      )}
      {capabilities?.length === 0 && (
        <div className={styles.loading}>NO CAPABILITIES REPORTED</div>
      )}
      <div className={styles.list}>
        {capabilities?.map((capability, index) => (
          <CapabilityCardBoundary
            key={`${capability.id}-${index}`}
            capability={capability}>
            <CapabilityCard capability={capability} now={now} />
          </CapabilityCardBoundary>
        ))}
      </div>
    </div>
  );
}
