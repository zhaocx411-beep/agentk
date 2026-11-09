/**
 * OfficialMarketDetail - 官方 Agent 详情落地页（自主规划 + 工作流）
 * 封面与目录卡同一套配图；名称 / 描述走官方目录接口
 */
import { useMemo } from 'react';
import { Button, Empty, Spin } from 'antd';
import { ArrowLeftOutlined, ArrowRightOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import { OFFICIAL_DEFAULTS_QUERY_KEY, getOfficialDefaults } from '../api';
import { toOfficialLoopCard, toOfficialWorkflowCard } from '../utils';
import { resolveOfficialCardVisual } from '../lib/officialCardCovers';
import { resolveMarketGuideSteps, resolveMarketGuideTitle } from '../lib/marketGuide';
import { useOfficialLaunch } from '../hooks/useOfficialLaunch';
import { useOfficialTemplate } from '../OfficialExperience/useOfficialTemplate';
import styles from './OfficialMarketDetail.module.less';

function OfficialMarketDetail() {
  const { cardKey = '' } = useParams();
  const navigate = useNavigate();
  const { launch, launching, modal } = useOfficialLaunch();

  const { data, isLoading } = useQuery({
    queryKey: OFFICIAL_DEFAULTS_QUERY_KEY,
    queryFn: getOfficialDefaults,
  });

  const item = useMemo(() => {
    const loops = (data?.agentLoops ?? []).map(toOfficialLoopCard);
    const workflows = (data?.workflowAgents ?? []).map(toOfficialWorkflowCard);
    return [...loops, ...workflows].find((card) => card.key === cardKey) ?? null;
  }, [data, cardKey]);

  const { usingTemplate, applyTemplate } = useOfficialTemplate(item);

  const visual = item ? resolveOfficialCardVisual(item) : null;
  const guideSteps = item ? resolveMarketGuideSteps(item) : [];
  const guideTitle = item ? resolveMarketGuideTitle(item) : '如何开始';
  const description = (item?.detail || item?.description || '').trim();
  const versionLabel = item?.skills.find((skill) => skill.startsWith('v'));
  const typeLabel = item?.variant === 'workflow' ? '工作流 Agent' : 'Agent-Loop';
  const isWorkflow = item?.variant === 'workflow';
  const launchBusy = launching;
  const bottomLead = isWorkflow ? '准备好了就直接开始创作' : '准备好了就直接开对话';

  if (isLoading) {
    return (
      <div className={styles.loading}>
        <Spin size="large" />
      </div>
    );
  }

  if (!item || !visual) {
    return (
      <div className={styles.loading}>
        <Empty description="未找到该 Agent">
          <Button type="link" onClick={() => navigate('/agents/official')}>
            返回可灵Agent中后台
          </Button>
        </Empty>
      </div>
    );
  }

  return (
    <div className={styles.page}>
      {modal}
      <div className={styles.shell}>
        <button type="button" className={styles.back} onClick={() => navigate('/agents/official')}>
          <ArrowLeftOutlined />
          可灵Agent中后台
        </button>

        <section className={styles.hero}>
          <img className={styles.heroPhoto} src={visual.cover} alt="" />
          <span className={styles.heroScrim} aria-hidden />
          <div className={styles.heroCopy}>
            <span className={styles.eyebrow}>
              {item.subtitle} · {typeLabel}
            </span>
            <h1 className={styles.title}>{item.name}</h1>
            {description ? <p className={styles.pitch}>{description}</p> : null}
            {versionLabel ? <span className={styles.version}>{versionLabel}</span> : null}
            <div className={styles.actions}>
              <Button type="primary" size="large" loading={launchBusy} onClick={() => launch(item)}>
                立即使用
                <ArrowRightOutlined />
              </Button>
              {!isWorkflow ? (
                <Button
                  size="large"
                  className={styles.ctaSecondary}
                  loading={usingTemplate}
                  onClick={() => void applyTemplate()}
                >
                  基于此创建
                </Button>
              ) : null}
            </div>
          </div>
        </section>

        <section className={styles.guide}>
          <h2 className={styles.sectionTitle}>{guideTitle}</h2>
          <ol className={styles.guideList}>
            {guideSteps.map((step) => (
              <li key={step.title} className={styles.guideItem}>
                <div className={styles.guideMedia}>
                  <img src={step.image} alt="" />
                </div>
                <div className={styles.guideCopy}>
                  <h3>{step.title}</h3>
                  <p>{step.description}</p>
                </div>
              </li>
            ))}
          </ol>
        </section>

        <div className={styles.bottomBar}>
          <p className={styles.bottomLead}>{bottomLead}</p>
          <Button type="primary" size="large" loading={launchBusy} onClick={() => launch(item)}>
            立即使用 {item.name}
            <ArrowRightOutlined />
          </Button>
        </div>
      </div>
    </div>
  );
}

export default OfficialMarketDetail;
