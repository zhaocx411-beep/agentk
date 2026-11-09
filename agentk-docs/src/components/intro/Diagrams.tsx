import type {ReactNode} from 'react';
import styles from './Diagrams.module.css';

function Diagram({
  title,
  path,
  children,
}: {
  title: string;
  path: string;
  children: ReactNode;
}): ReactNode {
  return (
    <figure className={styles.diagram}>
      <figcaption className={styles.diagramHead}>
        <strong>{title}</strong>
        <code>{path}</code>
      </figcaption>
      <div className={styles.diagramViewport}>{children}</div>
    </figure>
  );
}

function LifecycleSteps({
  items,
}: {
  items: Array<{name: string; detail: string}>;
}): ReactNode {
  return (
    <div className={styles.lifecycleSteps}>
      {items.map((item, index) => (
        <div className={styles.lifecycleStep} key={item.name}>
          {index > 0 && <span className={styles.lifecycleArrow}>→</span>}
          <div>
            <strong>{item.name}</strong>
            <span>{item.detail}</span>
          </div>
        </div>
      ))}
    </div>
  );
}

export function SystemTopology(): ReactNode {
  const apps = [
    {name: 'Agent App A', y: 210},
    {name: 'Agent App B', y: 280},
    {name: 'Agent App C', y: 350},
  ];
  const workers = [
    {name: 'Custom A', x: 475},
    {name: 'Custom B', x: 625},
    {name: 'Custom C', x: 775},
  ];

  return (
    <Diagram
      title="AgentK 系统拓扑与能力网络"
      path="SYSTEM CONTEXT · RUNTIME FLOW + CAPABILITY FLOW">
      <div className={styles.systemTopology}>
        <svg
          className={styles.topologySvg}
          viewBox="0 0 1000 840"
          role="img"
          aria-label="AgentK 系统拓扑与 Capability 供给消费网络"
          aria-describedby="system-topology-description"
        >
          <desc id="system-topology-description">
            Agent 应用通过 Client SDK 使用 AgentK。自定义 Worker 通过 Worker SDK
            发布 Capability，进入 Catalog 后可被多个应用选择和复用。
            规划中的 AgentK Platform 位于运行链路之外，提供管理、开发与观测能力。
          </desc>
          <defs>
            <marker
              id="topology-arrow-blue"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="5"
              markerHeight="5"
              orient="auto-start-reverse"
            >
              <path d="M 0 0 L 10 5 L 0 10 z" fill="#356d9d" />
            </marker>
            <marker
              id="topology-arrow-green"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="5"
              markerHeight="5"
              orient="auto"
            >
              <path d="M 0 0 L 10 5 L 0 10 z" fill="#39725b" />
            </marker>
            <marker
              id="topology-arrow-gray"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="5"
              markerHeight="5"
              orient="auto"
            >
              <path d="M 0 0 L 10 5 L 0 10 z" fill="#8994a0" />
            </marker>
          </defs>

          <g>
            <rect
              className={styles.plannedBoundary}
              x="400"
              y="18"
              width="550"
              height="78"
              rx="12"
            />
            <text className={styles.planeLabel} x="425" y="43">
              OPTIONAL CONTROL PLANE · PLANNED
            </text>
            <text className={styles.topologyTitle} x="425" y="70">
              AgentK Platform
            </text>
            <text className={styles.topologyCopy} x="605" y="69">
              Profiles · Playground · Capability 浏览 · 运行监控
            </text>
            <path
              className={styles.managementEdge}
              d="M675 96 V139"
              markerEnd="url(#topology-arrow-gray)"
            />
            <text className={styles.managementEdgeLabel} x="690" y="122">
              manage · develop · observe
            </text>
          </g>

          <g>
            <text className={styles.planeLabel} x="30" y="180">
              CONSUMER SYSTEMS
            </text>
            {apps.map((app) => (
              <g key={app.name}>
                <rect
                  className={styles.appNode}
                  x="30"
                  y={app.y}
                  width="200"
                  height="50"
                  rx="25"
                />
                <text
                  className={styles.topologyNodeTitle}
                  x="130"
                  y={app.y + 31}
                  textAnchor="middle"
                >
                  {app.name}
                </text>
              </g>
            ))}
            <text className={styles.topologyEllipsis} x="130" y="440" textAnchor="middle">
              · · ·
            </text>
          </g>

          <g>
            <rect
              className={styles.runtimeBoundary}
              x="300"
              y="140"
              width="650"
              height="365"
              rx="16"
            />
            <text className={styles.planeLabel} x="330" y="170">
              AGENTK RUNTIME
            </text>
            <text className={styles.topologyTitle} x="330" y="200">
              AgentK Server
            </text>
            <line className={styles.boundaryRule} x1="330" y1="215" x2="920" y2="215" />

            <rect className={styles.runtimeNode} x="330" y="235" width="135" height="125" rx="10" />
            <text className={styles.topologyNodeTitle} x="348" y="270">
              API · Auth
            </text>
            <text className={styles.topologyNodeDetail} x="348" y="304">
              Client SDK · gRPC
            </text>
            <text className={styles.topologyNodeDetail} x="348" y="328">
              identity boundary
            </text>

            <rect className={styles.runtimeNode} x="477" y="235" width="135" height="125" rx="10" />
            <text className={styles.topologyNodeTitle} x="495" y="270">
              Session
            </text>
            <text className={styles.topologyNodeDetail} x="495" y="304">
              Profile
            </text>
            <text className={styles.topologyNodeDetail} x="495" y="328">
              Events
            </text>

            <rect className={styles.runtimeNode} x="624" y="235" width="135" height="125" rx="10" />
            <text className={styles.topologyNodeTitle} x="642" y="270">
              Runtime
            </text>
            <text className={styles.topologyNodeDetail} x="642" y="304">
              Agent Loop
            </text>
            <text className={styles.topologyNodeDetail} x="642" y="328">
              Pipeline
            </text>

            <rect className={styles.catalogSvgNode} x="771" y="235" width="149" height="125" rx="10" />
            <text className={styles.capabilityLabel} x="789" y="261">
              SHARED SURFACE
            </text>
            <text className={styles.topologyNodeTitle} x="789" y="291">
              Catalog
            </text>
            <text className={styles.topologyNodeDetail} x="789" y="319">
              contract · capability ID
            </text>
            <text className={styles.topologyNodeDetail} x="789" y="341">
              target
            </text>

            <rect className={styles.coreSvgNode} x="330" y="405" width="590" height="50" rx="25" />
            <text className={styles.topologyNodeTitle} x="350" y="436">
              Core Worker
            </text>
            <text className={styles.topologyNodeDetail} x="900" y="436" textAnchor="end">
              loop · built-ins · routing
            </text>
          </g>

          <g>
            <rect className={styles.infraNode} x="160" y="580" width="220" height="125" rx="24" />
            <text className={styles.planeLabel} x="185" y="614">
              DURABLE EXECUTION
            </text>
            <text className={styles.topologyTitle} x="185" y="647">
              Temporal Service
            </text>
            <text className={styles.topologyNodeDetail} x="185" y="675">
              history · task queue · timer · retry
            </text>
          </g>

          <g>
            <rect
              className={styles.workerBoundary}
              x="450"
              y="550"
              width="500"
              height="185"
              rx="16"
            />
            <text className={styles.planeLabel} x="475" y="580">
              CAPABILITY SUPPLY
            </text>
            <text className={styles.topologyTitle} x="475" y="608">
              Custom Worker Fleet
            </text>

            {workers.map((worker) => (
              <g key={worker.name}>
                <rect
                  className={styles.workerNode}
                  x={worker.x}
                  y="625"
                  width="135"
                  height="70"
                  rx="8"
                />
                <circle className={styles.workerDot} cx={worker.x + 18} cy="647" r="4" />
                <text className={styles.topologyNodeTitle} x={worker.x + 30} y="652">
                  {worker.name}
                </text>
                <text className={styles.topologyNodeDetail} x={worker.x + 18} y="677">
                  Workflow / Activity
                </text>
              </g>
            ))}
          </g>

          <g>
            <path
              className={styles.runtimeEdge}
              d="M230 305 H280 V298 H330"
              markerStart="url(#topology-arrow-blue)"
              markerEnd="url(#topology-arrow-blue)"
            />
            <text className={styles.runtimeEdgeLabel} x="240" y="285">
              Client SDK
            </text>

            <path
              className={styles.runtimeEdge}
              d="M590 455 V490 H294 V580"
              markerStart="url(#topology-arrow-blue)"
              markerEnd="url(#topology-arrow-blue)"
            />
            <text className={styles.runtimeEdgeLabel} x="390" y="483">
              durable execution
            </text>

            <path
              className={styles.runtimeEdge}
              d="M380 642 H450"
              markerStart="url(#topology-arrow-blue)"
              markerEnd="url(#topology-arrow-blue)"
            />
            <text className={styles.runtimeEdgeLabel} x="395" y="627">
              task queues
            </text>
            <path className={styles.runtimeBus} d="M450 642 H460 V715 H842" />
            <path className={styles.runtimeBus} d="M542 715 V695 M692 715 V695 M842 715 V695" />
          </g>

          <g>
            <path
              className={styles.capabilityEdge}
              d="M842 695 V775 H270 V705"
              markerEnd="url(#topology-arrow-green)"
            />
            <text className={styles.capabilityEdgeLabel} x="650" y="764">
              Manifest announce
            </text>
            <path
              className={styles.capabilityEdge}
              d="M245 580 V535 H640 V450"
              markerEnd="url(#topology-arrow-green)"
            />
            <text className={styles.capabilityEdgeLabel} x="385" y="528">
              announce
            </text>
            <path
              className={styles.capabilityEdge}
              d="M846 410 V354"
              markerEnd="url(#topology-arrow-green)"
            />
            <path
              className={styles.capabilityEdge}
              d="M771 342 H755 V382 H544 V354"
              markerEnd="url(#topology-arrow-green)"
            />
            <text className={styles.capabilityEdgeLabel} x="655" y="376">
              capability ID
            </text>
            <path
              className={styles.capabilityEdge}
              d="M544 382 H310 C275 382 265 375 235 375"
              markerEnd="url(#topology-arrow-green)"
            />
            <text className={styles.capabilityEdgeLabel} x="280" y="370">
              list · select
            </text>
          </g>

          <g className={styles.topologyLegend}>
            <line className={styles.runtimeEdge} x1="30" y1="820" x2="68" y2="820" />
            <text x="78" y="824">运行调用与结果</text>
            <line className={styles.capabilityEdge} x1="250" y1="820" x2="288" y2="820" />
            <text x="298" y="824">Capability 供给、发现与选择</text>
            <line className={styles.managementEdge} x1="560" y1="820" x2="598" y2="820" />
            <text x="608" y="824">管理与观测</text>
          </g>
        </svg>

        <div className={styles.topologySemantic} aria-hidden="true">
          <div>
            <code>CONTROL PLANE · PLANNED</code>
            <strong>AgentK Platform</strong>
            <span>管理、开发与观测，不参与应用的必经运行链路</span>
          </div>
          <div>
            <code>RUNTIME FLOW</code>
            <strong>Agent Apps → AgentK Server → Temporal → Workers</strong>
            <span>统一 API、持久化执行、Task Queue 与结果事件</span>
          </div>
          <div>
            <code>CAPABILITY FLOW</code>
            <strong>Custom Worker → Worker SDK announce → Catalog</strong>
            <span>裸 Capability ID 进入 Profile，供多个 Agent 应用选择和复用</span>
          </div>
        </div>

        <div className={styles.topologyCaption}>
          应用通过蓝色运行路径调用能力；Worker 通过绿色供给路径发布 Capability。
          能力进入共享目录后，可被不同应用按 Capability ID 组合复用。
        </div>
      </div>
    </Diagram>
  );
}

export function PlatformEvolution(): ReactNode {
  const modelLifecycle = [
    {name: '训练', detail: 'Model Artifact'},
    {name: '评测 / A/B', detail: 'Quality Signals'},
    {name: '版本 / 发布', detail: 'Model Version'},
    {name: 'Serve', detail: 'Inference Endpoint'},
  ];
  const agentLifecycle = [
    {name: '设计 / 配置', detail: 'Profile · Prompt · Capability'},
    {name: '实验 / 评测', detail: 'Runs · Dataset · Metrics'},
    {name: '版本 / 发布', detail: 'Agent Release'},
    {name: '运行 / 观测', detail: 'Session · Events · Artifacts'},
  ];

  return (
    <Diagram
      title="从模型生命周期到 Agent 生命周期"
      path="PRODUCTION OBJECT: MODEL VERSION → AGENT RELEASE">
      <div className={styles.platformEvolution}>
        <div className={styles.lifecycleRow}>
          <div className={styles.lifecycleLabel}>
            <code>NN ERA</code>
            <strong>模型平台</strong>
            <span>管理模型版本</span>
          </div>
          <LifecycleSteps items={modelLifecycle} />
        </div>

        <div className={styles.objectShift}>
          <span>生产对象迁移</span>
          <strong>Model Version → Agent Release</strong>
        </div>

        <div className={`${styles.lifecycleRow} ${styles.agentLifecycle}`}>
          <div className={styles.lifecycleLabel}>
            <code>AGENT ERA</code>
            <strong>AgentK Platform</strong>
            <span>管理可执行 Agent 版本</span>
          </div>
          <LifecycleSteps items={agentLifecycle} />
        </div>

        <div className={styles.platformLayers}>
          <div>
            <code>CONTROL PLANE · PLANNED</code>
            <strong>AgentK 管理平台</strong>
            <span>定义与资源目录 · 实验评测 · 版本发布 · 运行管理</span>
          </div>
          <div>
            <code>DATA PLANE · CURRENT</code>
            <strong>AgentK Runtime</strong>
            <span>Loop / Pipeline · Capability · Session · Events · Artifacts</span>
          </div>
        </div>

        <div className={styles.platformDependencies}>
          <strong>上游依赖</strong>
          <span>模型训练与 Serve 平台提供 LLM、Image、Video、Media Endpoint</span>
          <strong>下游形态</strong>
          <span>在线应用、内部直接使用、评测设计与批处理系统</span>
        </div>
      </div>
      <div className={styles.platformCaption}>
        <strong>当前边界：</strong>AgentK 已实现 Runtime 数据面；管理与生命周期控制面是后续平台方向。
      </div>
    </Diagram>
  );
}

function LayerLabel({
  layer,
  children,
}: {
  layer: string;
  children: ReactNode;
}): ReactNode {
  return (
    <div className={styles.capLabel}>
      {layer}
      <b>{children}</b>
    </div>
  );
}

function DownArrows({split = false}: {split?: boolean}): ReactNode {
  return (
    <div className={`${styles.downArrows} ${split ? styles.splitTargets : ''}`}>
      <div />
      <div>
        <span className={styles.downArrow} />
        {split && <span />}
        <span className={styles.downArrow} />
      </div>
    </div>
  );
}

export function CapabilityArchitecture(): ReactNode {
  const atomic = [
    ['Text', 'generate_text'],
    ['Image / Edit', 'generate_image'],
    ['Video', 'generate_video'],
    ['Understand', 'understand_media'],
    ['Compose', 'concat_videos'],
    ['HTTP', 'http_call'],
  ];

  return (
    <Diagram
      title="Capability、Tool 与 Pipeline"
      path="EXPOSURE → DISPATCH → CAPABILITY → TEMPORAL">
      <div className={styles.capabilityMap}>
        <div className={styles.capRow}>
          <LayerLabel layer="Layer 04">消费面</LayerLabel>
          <div className={styles.exposureGrid}>
            <div className={styles.exposure}>
              <div>
                <code>TOOL</code>
                <strong>LLM 调用面</strong>
                <span>可选声明 · blocking execution</span>
              </div>
              <div>
                <code>AGENT LOOP</code>
                <strong>模型动态选择</strong>
                <span>消息驱动，结果回到上下文</span>
              </div>
            </div>
            <div className={styles.exposure}>
              <div>
                <code>PIPELINE</code>
                <strong>Client SDK 调用面</strong>
                <span>Session name · Execution lifecycle</span>
              </div>
              <div>
                <code>WORKFLOW</code>
                <strong>调用方显式选择</strong>
                <span>参数驱动，按确定拓扑推进</span>
              </div>
            </div>
          </div>
        </div>

        <DownArrows />

        <div className={styles.capRow}>
          <LayerLabel layer="Layer 03">能力选择</LayerLabel>
          <div className={styles.dispatch}>
            <div>
              <strong>Capability Dispatch</strong>
              <code>stable implementation ABI</code>
            </div>
            <div>
              <span>
                Tool 与 Pipeline 都按唯一 Capability ID 选择原子或组合能力；Pipeline 不需要发布声明。
              </span>
            </div>
          </div>
        </div>

        <DownArrows split />

        <div className={styles.capRow}>
          <LayerLabel layer="Layer 02">Capability 域</LayerLabel>
          <div className={styles.capabilityPlane}>
            <div className={styles.capGroup}>
              <div className={styles.capGroupHead}>
                <strong>组合能力</strong>
                <code>WORKFLOW</code>
              </div>
              <div className={styles.compositeItems}>
                <div className={styles.capabilityItem}>
                  <strong>Storyboard Film</strong>
                  <code>storyboard_film</code>
                </div>
                <div className={styles.capabilityItem}>
                  <strong>Brand Ad</strong>
                  <code>generate_brand_ad</code>
                </div>
              </div>
            </div>
            <div className={styles.composeArrow}>
              →<span>COMPOSE<br />BY ABI</span>
            </div>
            <div className={`${styles.capGroup} ${styles.atomic}`}>
              <div className={styles.capGroupHead}>
                <strong>原子能力</strong>
                <code>ACTIVITY / FUNCTION</code>
              </div>
              <div className={styles.atomicItems}>
                {atomic.map(([name, id]) => (
                  <div className={styles.capabilityItem} key={id}>
                    <strong>{name}</strong>
                    <code>{id}</code>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>

        <DownArrows split />

        <div className={styles.capRow}>
          <LayerLabel layer="Layer 01">执行底座</LayerLabel>
          <div className={styles.temporalBase}>
            <div><strong>Temporal</strong><span>durable execution</span></div>
            <div><strong>Task Queue</strong><span>分布式调度</span></div>
            <div><strong>History</strong><span>状态恢复</span></div>
            <div><strong>Timer / Retry</strong><span>等待与失败策略</span></div>
            <div><strong>Child Workflow</strong><span>组合与并发</span></div>
          </div>
        </div>

        <div className={styles.crossCut}>
          <div>Profile · 暴露面</div>
          <div>Event Log · 对外事实</div>
          <div>Artifacts · 产物引用</div>
          <div>Auth · 接入方身份</div>
        </div>
      </div>
    </Diagram>
  );
}

function PathNode({
  tone,
  title,
  children,
}: Children & {
  tone?: 'runtime' | 'capability' | 'execution';
  title: string;
}): ReactNode {
  return (
    <div className={`${styles.pathNode} ${tone ? styles[tone] : ''}`}>
      <strong>{title}</strong>
      <span>{children}</span>
    </div>
  );
}

type Children = {children: ReactNode};

export function RuntimePath(): ReactNode {
  return (
      <Diagram
        title="稳定运行路径"
        path="CALLER → SESSION → EXECUTION → CAPABILITY → RESULTS">
      <div className={styles.runtimePath}>
        <div className={styles.callerRow}>
          <PathNode title="下游应用服务">已登记 KSN · Service · App</PathNode>
          <PathNode title="评测 / 批处理平台">独立 KSN Service · App 隔离</PathNode>
          <PathNode title="业务调用方">下游鉴权 · Session 映射</PathNode>
        </div>
        <div className={styles.pathArrow}>↓</div>
        <PathNode tone="runtime" title="AgentK Client SDK">
          发现能力 · 创建 Session · 启动 Execution · 读取事件
        </PathNode>
        <div className={styles.pathArrow}>↓</div>
        <PathNode tone="runtime" title="Session + Profile">
          system · model · capability IDs · Session names
        </PathNode>
        <div className={styles.pathArrow}>↓</div>
        <div className={styles.pathBranch}>
          <PathNode tone="runtime" title="Agent Execution">
            消息进入模型循环，Tool 由模型动态选择
          </PathNode>
          <PathNode tone="runtime" title="Pipeline Execution">
            调用方显式选择流程，Workflow 确定拓扑
          </PathNode>
        </div>
        <div className={styles.pathArrow}>↓</div>
        <PathNode tone="capability" title="Capability">
          Catalog 解析契约与路由 · 原子 Activity / 组合 Workflow
        </PathNode>
        <div className={styles.pathArrow}>↓</div>
        <PathNode tone="execution" title="Serve / Worker">
          模型与媒体 Serve · 业务 HTTP 服务 · 内置或独立 Temporal Worker
        </PathNode>
        <div className={styles.pathArrow}>↓</div>
        <div className={styles.pathResults}>
          <PathNode title="ListEvents">Session 持久事件 · 历史与恢复</PathNode>
          <PathNode title="WatchEvents">当前 Execution · 实时更新</PathNode>
          <PathNode title="GetExecution">状态 · 错误 · Pipeline 输出</PathNode>
          <PathNode title="Artifacts">图片、视频等产物 URI</PathNode>
        </div>
      </div>
    </Diagram>
  );
}
