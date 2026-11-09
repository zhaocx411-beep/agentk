import type {ReactNode} from 'react';
import styles from './RuntimeDiagrams.module.css';

type Children = {children: ReactNode};

function Diagram({
  title,
  path,
  children,
}: Children & {
  title: string;
  path: string;
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

function RuntimeNode({
  kind,
  title,
  children,
}: Children & {
  kind: string;
  title: string;
}): ReactNode {
  return (
    <div className={styles.runtimeNode}>
      <code>{kind}</code>
      <strong>{title}</strong>
      <span>{children}</span>
    </div>
  );
}

function Exchange({
  forward,
  backward,
}: {
  forward: string;
  backward: string;
}): ReactNode {
  return (
    <div className={styles.exchange}>
      <span>↓ {forward}</span>
      <span>↑ {backward}</span>
    </div>
  );
}

export function RuntimeOverview(): ReactNode {
  return (
    <Diagram
      title="AgentK 运行闭环"
      path="INPUT → SESSION → EXECUTION ↔ CAPABILITY → EVENTS">
      <div className={styles.runtimeOverview}>
        <div className={styles.runtimeFlow}>
          <RuntimeNode kind="CALLER" title="调用方">
            Message · Answers · Pipeline
          </RuntimeNode>

          <div className={styles.oneWay}>
            <span>追加意图</span>
            <b>↓</b>
          </div>

          <div className={styles.sessionBoundary}>
            <div className={styles.boundaryHead}>
              <code>SESSION BOUNDARY</code>
              <strong>长期任务边界</strong>
            </div>
            <div className={styles.sessionParts}>
              <div>
                <code>FROZEN PROFILE</code>
                <strong>行为与暴露面</strong>
              </div>
              <div>
                <code>EVENT LOG</code>
                <strong>只追加的事实</strong>
              </div>
            </div>
          </div>

          <Exchange forward="投影下一步" backward="追加事实" />

          <RuntimeNode kind="TEMPORAL" title="Runtime">
            推进 · 等待 · 重试 · 恢复
          </RuntimeNode>

          <Exchange forward="调度" backward="返回结果" />

          <RuntimeNode kind="DOMAIN" title="Capability">
            Activity · Workflow
          </RuntimeNode>
        </div>

        <div className={styles.resultFlow}>
          <div>
            <code>FROM EVENT LOG</code>
            <strong>同一事实，不同投影</strong>
          </div>
          <span>ListEvents · WatchEvents</span>
          <span>Execution Status / Output</span>
          <span>Tool / Pipeline Artifacts</span>
          <span>Model Context</span>
        </div>
      </div>
    </Diagram>
  );
}

function StateStage({
  label,
  children,
}: Children & {
  label: string;
}): ReactNode {
  return (
    <div className={styles.stateStage}>
      <code>{label}</code>
      <span>{children}</span>
    </div>
  );
}

function StateLane({
  kind,
  title,
  question,
  children,
}: Children & {
  kind: 'runtime' | 'log';
  title: string;
  question: string;
}): ReactNode {
  return (
    <div className={`${styles.stateLane} ${styles[kind]}`}>
      <div className={styles.stateLaneHead}>
        <code>{kind === 'runtime' ? 'EXECUTION STATE' : 'BUSINESS FACTS'}</code>
        <strong>{title}</strong>
        <span>{question}</span>
      </div>
      <div className={styles.stateLaneFlow}>{children}</div>
    </div>
  );
}

export function StateTracks(): ReactNode {
  return (
    <Diagram
      title="执行现场与对外事实"
      path="ONE TASK · TWO RESPONSIBILITIES">
      <div className={styles.stateTracks}>
        <StateLane
          kind="runtime"
          title="Temporal Workflow History"
          question="任务怎样可靠地继续？">
          <StateStage label="来源">Agent Execution · Pipeline Execution</StateStage>
          <b>→</b>
          <StateStage label="保存">执行位置 · Timer · Retry · Cancel</StateStage>
          <b>→</b>
          <StateStage label="服务">重放 · 等待 · 恢复</StateStage>
        </StateLane>

        <div className={styles.responsibilitySplit}>
          <span>同一个任务</span>
          <strong>执行恢复 ≠ 业务观察</strong>
        </div>

        <StateLane
          kind="log"
          title="Session Event Log"
          question="任务已经发生了什么？">
          <StateStage label="来源">API 输入 · Runtime 结果</StateStage>
          <b>→</b>
          <StateStage label="保存">Input · Round · Tool · Execution · Progress</StateStage>
          <b>→</b>
          <StateStage label="服务">历史 · 恢复 · 状态 · 模型上下文</StateStage>
        </StateLane>
      </div>
    </Diagram>
  );
}
