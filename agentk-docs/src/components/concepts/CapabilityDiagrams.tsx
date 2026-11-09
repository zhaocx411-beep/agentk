import type {ReactNode} from 'react';
import styles from './CapabilityDiagrams.module.css';

function FlowNode({
  label,
  title,
  children,
}: {
  label: string;
  title: string;
  children: ReactNode;
}): ReactNode {
  return (
    <div className={styles.flowNode}>
      <code>{label}</code>
      <strong>{title}</strong>
      <span>{children}</span>
    </div>
  );
}

function Lane({
  label,
  title,
  children,
}: {
  label: string;
  title: string;
  children: ReactNode;
}): ReactNode {
  return (
    <section className={styles.lane}>
      <header>
        <code>{label}</code>
        <strong>{title}</strong>
      </header>
      <div className={styles.laneBody}>{children}</div>
    </section>
  );
}

function Arrow({children}: {children: ReactNode}): ReactNode {
  return (
    <div className={styles.arrow}>
      <span>{children}</span>
      <b>↓</b>
    </div>
  );
}

export function CapabilityLifecycle(): ReactNode {
  return (
    <figure className={styles.diagram}>
      <figcaption className={styles.diagramHead}>
        <strong>Capability 从供给到消费</strong>
        <code>DECLARE ONCE → DISCOVER → SELECT BY ID → CALL</code>
      </figcaption>
      <div className={styles.diagramViewport}>
        <div className={styles.flow}>
          <Lane label="PROVIDER" title="能力提供方">
            <FlowNode label="IMPLEMENTATION" title="Workflow / Activity">
              领域逻辑与业务 ABI
            </FlowNode>
            <FlowNode label="NATIVE REGISTRATION" title="Temporal Worker">
              注册 type 并监听 task queue
            </FlowNode>
            <FlowNode label="PUBLICATION" title="Capability Manifest">
              唯一 ID · Schema · timeout · 可选 Tool
            </FlowNode>
          </Lane>

          <Arrow>周期上报</Arrow>

          <Lane label="AGENTK" title="标准消费面">
            <FlowNode label="DISCOVERY" title="Capability Catalog">
              当前环境的可用目录
            </FlowNode>
            <FlowNode label="PUBLIC CONTRACT" title="Capability API">
              返回公开契约与唯一 Capability ID
            </FlowNode>
            <div className={styles.surfaceRoutes}>
              <FlowNode label="TOOL RUNTIME" title="Agent Loop">
                模型选择 · Tool 调度
              </FlowNode>
              <FlowNode label="PIPELINE RUNTIME" title="Pipeline Container">
                显式调用 · 参数校验
              </FlowNode>
              <FlowNode label="DIRECT INVOKE" title="Transient Wrapper">
                阻塞调用 · 直接返回
              </FlowNode>
            </div>
          </Lane>

          <Arrow>选择 ID</Arrow>

          <Lane label="CONSUMER" title="下游应用">
            <FlowNode label="CALL MODE" title="Direct / Session">
              裸 capability ID · Profile 内名称
            </FlowNode>
            <FlowNode label="CLIENT SDK" title="调用入口">
              InvokeCapability · StartExecution
            </FlowNode>
            <FlowNode label="RESULTS" title="结果与状态">
              RPC Output · Events · Execution
            </FlowNode>
          </Lane>
        </div>

        <div className={styles.runtimeReturn}>
          <span>运行时调用</span>
          <strong>Capability ID → Catalog target → Direct / Agent Loop / Pipeline → Temporal</strong>
          <span>结果返回</span>
        </div>
      </div>
    </figure>
  );
}
