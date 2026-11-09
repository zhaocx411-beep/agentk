/**
 * 平台首页 — 官方自主规划 / 工作流展示 + 站内模块跳转入口
 */
import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { CardGridSkeleton } from '@/components';
import { OFFICIAL_DEFAULTS_QUERY_KEY, getOfficialDefaults } from '@/pages/AgentCenter/api';
import {
  HOME_COPY,
  HOME_ENTRY_CARDS,
  HOME_HERO_COLLAGE,
  HOME_TOOLKIT_CARDS,
  type HomeAgentCard,
} from './mock';
import { mapOfficialLoopsToHomeCards } from './mapOfficialLoops';
import { mapOfficialWorkflowsToHomeCards } from './mapOfficialWorkflows';
import { ModelShowcase } from './components/ModelShowcase';
import { HomeWorksSection } from './components/HomeWorksSection';
import styles from './Home.module.less';

function AgentCardView({ card, onLive }: { card: HomeAgentCard; onLive: (href: string) => void }) {
  const isPlaceholder = card.status === 'placeholder';
  const isLoop = card.visual === 'loop';
  const isClickable = card.status === 'live' && Boolean(card.href);

  return (
    <article
      className={`${styles.agentCard} ${isPlaceholder ? styles.agentCardPlaceholder : ''} ${isLoop ? styles.agentCardLoop : ''} ${isClickable ? styles.agentCardClickable : ''}`}
    >
      {isLoop ? (
        <div className={styles.loopCover} aria-hidden>
          <span className={styles.loopSpark} />
          <div className={styles.loopCoverTitle}>{card.title}</div>
          <div className={styles.loopPills}>
            <span className={styles.loopPillActive}>Agent</span>
            <span className={styles.loopPill}>官方示范</span>
          </div>
        </div>
      ) : !isPlaceholder && card.cover ? (
        <div className={styles.agentMedia}>
          <img className={styles.agentCover} src={card.cover} alt="" />
          {card.thumb ? <img className={styles.agentThumb} src={card.thumb} alt="" /> : null}
        </div>
      ) : (
        <div className={styles.agentMedia} aria-hidden />
      )}

      <div className={styles.agentBody}>
        <div>
          {!isLoop ? <h3 className={styles.agentTitle}>{card.title}</h3> : null}
          <p
            className={`${styles.agentDesc} ${isLoop ? styles.agentDescLoop : ''}`}
            title={card.description}
          >
            {card.description}
          </p>
        </div>
      </div>

      <hr className={styles.divider} />

      <div className={styles.agentFooter}>
        {!isLoop ? <div className={styles.price}>{card.priceLabel}</div> : <div />}
        {card.status === 'live' && card.href ? (
          <button
            type="button"
            className={`${styles.cta} ${isLoop ? styles.ctaPrimary : ''}`}
            onClick={() => onLive(card.href!)}
          >
            {card.cta}
            <span className={styles.ctaArrow} aria-hidden />
          </button>
        ) : (
          <button type="button" className={`${styles.cta} ${styles.ctaSoon}`} disabled>
            {card.cta}
          </button>
        )}
      </div>
    </article>
  );
}

function Home() {
  const navigate = useNavigate();
  const { data, isLoading } = useQuery({
    queryKey: OFFICIAL_DEFAULTS_QUERY_KEY,
    queryFn: getOfficialDefaults,
    staleTime: 5 * 60 * 1000,
  });

  const loopCards = useMemo(
    () => mapOfficialLoopsToHomeCards(data?.agentLoops),
    [data?.agentLoops],
  );

  const workflowCards = useMemo(
    () => mapOfficialWorkflowsToHomeCards(data?.workflowAgents),
    [data?.workflowAgents],
  );

  return (
    <div className={styles.page}>
      <div className={styles.main}>
        {/* Hero */}
        <section className={styles.hero}>
          <div className={styles.heroCopy}>
            <p className={styles.heroEyebrow}>{HOME_COPY.hero.eyebrow}</p>
            <h1 className={styles.heroTitle}>{HOME_COPY.hero.title}</h1>
            <p className={styles.heroDesc}>{HOME_COPY.hero.desc}</p>
            <div className={styles.heroActions}>
              <button
                type="button"
                className={styles.cta}
                onClick={() => navigate(HOME_COPY.hero.primaryHref)}
              >
                {HOME_COPY.hero.primaryCta}
                <span className={styles.ctaArrow} aria-hidden />
              </button>
              <button
                type="button"
                className={styles.ctaGhost}
                onClick={() => navigate(HOME_COPY.hero.secondaryHref)}
              >
                {HOME_COPY.hero.secondaryCta}
              </button>
            </div>
          </div>
          <div className={styles.heroCollage} aria-hidden>
            <img
              className={`${styles.heroCollageItem} ${styles.heroCollageItem1}`}
              src={HOME_HERO_COLLAGE[0]}
              alt=""
            />
            <img
              className={`${styles.heroCollageItem} ${styles.heroCollageItem2}`}
              src={HOME_HERO_COLLAGE[1]}
              alt=""
            />
            <img
              className={`${styles.heroCollageItem} ${styles.heroCollageItem3}`}
              src={HOME_HERO_COLLAGE[2]}
              alt=""
            />
            <img
              className={`${styles.heroCollageItem} ${styles.heroCollageItem4}`}
              src={HOME_HERO_COLLAGE[3]}
              alt=""
            />
          </div>
        </section>

        {/* 自主规划 Agent Loop */}
        {(isLoading && !data) || loopCards.length > 0 ? (
          <section className={styles.section} aria-labelledby="home-loop-title">
            <header className={`${styles.sectionHead} ${styles.sectionHeadRow}`}>
              <div>
                <h2 id="home-loop-title" className={styles.sectionTitle}>
                  {HOME_COPY.loopSection.title}
                </h2>
                <p className={styles.sectionDesc}>{HOME_COPY.loopSection.desc}</p>
              </div>
              <button
                type="button"
                className={styles.ctaGhost}
                onClick={() => navigate(HOME_COPY.loopSection.moreHref)}
              >
                {HOME_COPY.loopSection.moreCta}
                <span className={styles.ctaArrow} aria-hidden />
              </button>
            </header>
            {isLoading && !data ? (
              <CardGridSkeleton className={styles.agentGrid} variant="info" count={3} />
            ) : (
              <div className={styles.agentGrid}>
                {loopCards.map((card) => (
                  <AgentCardView key={card.id} card={card} onLive={(href) => navigate(href)} />
                ))}
              </div>
            )}
          </section>
        ) : null}

        {/* 官方工作流 */}
        {(isLoading && !data) || workflowCards.length > 0 ? (
          <section className={styles.section} aria-labelledby="home-agent-title">
            <header className={`${styles.sectionHead} ${styles.sectionHeadRow}`}>
              <div>
                <h2 id="home-agent-title" className={styles.sectionTitle}>
                  {HOME_COPY.agentSection.title}
                </h2>
                <p className={styles.sectionDesc}>{HOME_COPY.agentSection.desc}</p>
              </div>
              <button
                type="button"
                className={styles.ctaGhost}
                onClick={() => navigate(HOME_COPY.agentSection.moreHref)}
              >
                {HOME_COPY.agentSection.moreCta}
                <span className={styles.ctaArrow} aria-hidden />
              </button>
            </header>
            {isLoading && !data ? (
              <CardGridSkeleton className={styles.agentGrid} variant="info" count={3} />
            ) : (
              <div className={styles.agentGrid}>
                {workflowCards.map((card) => (
                  <AgentCardView key={card.id} card={card} onLive={(href) => navigate(href)} />
                ))}
              </div>
            )}
          </section>
        ) : null}

        {/* 平台入口 */}
        <section className={styles.section} aria-labelledby="home-entry-title">
          <header className={styles.sectionHead}>
            <h2 id="home-entry-title" className={styles.sectionTitle}>
              {HOME_COPY.entrySection.title}
            </h2>
            <p className={styles.sectionDesc}>{HOME_COPY.entrySection.desc}</p>
          </header>
          <div className={styles.entryGrid}>
            {HOME_ENTRY_CARDS.map((card) => (
              <button
                key={card.id}
                type="button"
                className={styles.entryCard}
                onClick={() => navigate(card.href)}
              >
                <div className={styles.entryMedia}>
                  <img src={card.cover} alt="" />
                  <span className={styles.entryTag}>{card.tag}</span>
                </div>
                <div className={styles.entryBody}>
                  <h3 className={styles.entryTitle}>{card.title}</h3>
                  <p className={styles.entryDesc}>{card.description}</p>
                  <span className={styles.entryCta}>
                    {card.cta}
                    <span className={styles.ctaArrow} aria-hidden />
                  </span>
                </div>
              </button>
            ))}
          </div>
        </section>

        <HomeWorksSection />

        {/* 视频 / 图像模型能力展示 */}
        <ModelShowcase />

        {/* 效率工具 */}
        <section className={styles.section} aria-labelledby="home-toolkit-title">
          <header className={styles.sectionHead}>
            <h2 id="home-toolkit-title" className={styles.sectionTitle}>
              {HOME_COPY.toolkitSection.title}
            </h2>
            <p className={styles.sectionDesc}>{HOME_COPY.toolkitSection.desc}</p>
          </header>
          <div className={styles.toolkitGrid}>
            {HOME_TOOLKIT_CARDS.map((card) => (
              <button
                key={card.id}
                type="button"
                className={styles.toolkitCard}
                onClick={() => navigate(card.href)}
              >
                <img className={styles.toolkitCover} src={card.cover} alt="" aria-hidden />
                <div className={styles.toolkitBody}>
                  <h3 className={styles.toolkitTitle}>{card.title}</h3>
                  <p className={styles.toolkitDesc}>{card.description}</p>
                  <span className={styles.entryCta}>
                    进入
                    <span className={styles.ctaArrow} aria-hidden />
                  </span>
                </div>
              </button>
            ))}
          </div>
        </section>
      </div>
    </div>
  );
}

export default Home;
