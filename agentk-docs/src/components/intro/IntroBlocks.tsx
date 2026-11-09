import type {ReactNode} from 'react';
import styles from './IntroBlocks.module.css';

type Children = {children: ReactNode};

export function IntroHeader({
  title,
  runtime,
  children,
}: Children & {title: string; runtime: string}): ReactNode {
  return (
    <header className={styles.introHeader}>
      <div className={styles.docType}>
        AgentK / Overview
      </div>
      <h1>{title}</h1>
      <div className={styles.runtimeType}>{runtime}</div>
      {children}
      <div className={styles.meta}>
        <span>CAPABILITY SUPPLY + CONSUMPTION</span>
        <span>DISTRIBUTED + DURABLE</span>
        <span>AGENT LOOP + PIPELINE</span>
        <span>SESSION + EVENTS</span>
      </div>
    </header>
  );
}

export function Lead({children}: Children): ReactNode {
  return <div className={styles.lead}>{children}</div>;
}

export function Thesis({children}: Children): ReactNode {
  return <div className={styles.thesis}>{children}</div>;
}

export function SectionLabel({children}: Children): ReactNode {
  return <div className={styles.sectionLabel}>{children}</div>;
}

export function SectionSummary({children}: Children): ReactNode {
  return <div className={styles.sectionSummary}>{children}</div>;
}

export function Content({children}: Children): ReactNode {
  return <div className={styles.content}>{children}</div>;
}

export function TwoModes({children}: Children): ReactNode {
  return <div className={styles.twoModes}>{children}</div>;
}

export function ModeCard({
  kind,
  title,
  flow,
  loop = false,
  children,
}: Children & {
  kind: string;
  title: string;
  flow: string[];
  loop?: boolean;
}): ReactNode {
  return (
    <article className={styles.modeCard}>
      <code>{kind}</code>
      <h3>{title}</h3>
      <div className={styles.miniFlow}>
        {flow.map((item, index) => (
          <span key={`${item}-${index}`}>
            {index > 0 && <i>→</i>}
            <b>{item}</b>
          </span>
        ))}
        {loop && <i>↻</i>}
      </div>
      <div className={styles.cardCopy}>{children}</div>
    </article>
  );
}

export function Conclusion({children}: Children): ReactNode {
  return <div className={styles.conclusion}>{children}</div>;
}

export function PainList({children}: Children): ReactNode {
  return <div className={styles.painList}>{children}</div>;
}

export function PainRow({
  number,
  title,
  children,
}: Children & {number: string; title: string}): ReactNode {
  return (
    <div className={styles.painRow}>
      <code>{number}</code>
      <strong>{title}</strong>
      <div className={styles.rowCopy}>{children}</div>
    </div>
  );
}

export function PullQuote({children}: Children): ReactNode {
  return <div className={styles.pullQuote}>{children}</div>;
}

export function Definition({
  term,
  children,
}: Children & {term: string}): ReactNode {
  return (
    <div className={styles.definition}>
      <code>CORE ABSTRACTION</code>
      <strong>{term}</strong> {children}
    </div>
  );
}

export function PositionGrid({children}: Children): ReactNode {
  return <div className={styles.positionGrid}>{children}</div>;
}

export function PositionCard({
  kind,
  title,
  children,
}: Children & {kind: string; title: string}): ReactNode {
  return (
    <article className={styles.positionCard}>
      <code>{kind}</code>
      <h3>{title}</h3>
      <div className={styles.cardCopy}>{children}</div>
    </article>
  );
}

export function StateModel({children}: Children): ReactNode {
  return (
    <div className={styles.stateModel}>
      <div>STATE MODEL</div>
      {children}
    </div>
  );
}

export function StateCell({
  title,
  children,
}: Children & {title: string}): ReactNode {
  return (
    <div>
      <strong>{title}</strong>
      <div className={styles.stateCopy}>{children}</div>
    </div>
  );
}

export function BoundaryNote({children}: Children): ReactNode {
  return <div className={styles.boundaryNote}>{children}</div>;
}

export function ModeComparison({children}: Children): ReactNode {
  return <div className={styles.modeComparison}>{children}</div>;
}

export function ModeDetail({
  kind,
  title,
  items,
}: {
  kind: string;
  title: string;
  items: string[];
}): ReactNode {
  return (
    <article className={styles.modeDetail}>
      <header>
        <code>{kind}</code>
        <h3>{title}</h3>
      </header>
      <ul>
        {items.map((item) => (
          <li key={item}>{item}</li>
        ))}
      </ul>
    </article>
  );
}

export function SharedRuntime(): ReactNode {
  return (
    <div className={styles.sharedRuntime}>
      <div>SHARED RUNTIME</div>
      <div>Capability</div>
      <div>Temporal</div>
      <div>Session Event Log</div>
      <div>Artifacts</div>
    </div>
  );
}

export function IntegrationGrid({children}: Children): ReactNode {
  return <div className={styles.integrationGrid}>{children}</div>;
}

export function IntegrationCard({
  kind,
  title,
  items,
}: {
  kind: string;
  title: string;
  items: string[];
}): ReactNode {
  return (
    <article className={styles.integrationCard}>
      <header>
        <code>{kind}</code>
        <strong>{title}</strong>
      </header>
      <ol>
        {items.map((item) => (
          <li key={item}>{item}</li>
        ))}
      </ol>
    </article>
  );
}

export function IdentityBoundary(): ReactNode {
  return (
    <div className={styles.identity}>
      <div>IDENTITY BOUNDARY</div>
      <div>
        <strong>AgentK 接入方</strong>
        <span>已登记的工作负载 KSN 解析为 Service，并归属于一个 App。</span>
      </div>
      <div>
        <strong>下游终端用户</strong>
        <span>
          用户资料、权限、配额、业务对象及其 Session 映射由下游应用管理。
        </span>
      </div>
    </div>
  );
}

export function TopicGrid({children}: Children): ReactNode {
  return <div className={styles.topicGrid}>{children}</div>;
}

export function Topic({
  number,
  title,
  children,
}: Children & {number: string; title: string}): ReactNode {
  return (
    <div className={styles.topic}>
      <code>{number}</code>
      <div>
        <strong>{title}</strong>
        <span>{children}</span>
      </div>
    </div>
  );
}

export function Closing({children}: Children): ReactNode {
  return <div className={styles.closing}>{children}</div>;
}

export function DocumentFooter(): ReactNode {
  return (
    <footer className={styles.footer}>
      AgentK · Overview · 2026
    </footer>
  );
}
