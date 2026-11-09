/**
 * OfficialFeaturedCarousel - 官方 Agent 精选旗舰轮播
 */
import { useCallback, useEffect, useRef, useState, type CSSProperties } from 'react';
import { Button } from 'antd';
import {
  ApartmentOutlined,
  ArrowRightOutlined,
  LeftOutlined,
  RightOutlined,
} from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { AgentMarkIcon } from '@/components';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import { FEATURED_AUTOPLAY_MS, FEATURED_FADE_MS } from '../../constants';
import { OFFICIAL_GALLERY_TEXT } from '../../mock';
import { buildOfficialMarketPath } from '../../utils';
import { officialCoverThemeIndex } from '../../lib/officialCardTheme';
import { buildFeaturedSlideMeta } from '../../lib/officialFeaturedMeta';
import { FEATURED_SLIDE_BACKGROUNDS } from '../../lib/featuredVisuals';
import FeaturedAgentVisual from '../FeaturedAgentVisual';
import styles from './OfficialFeaturedCarousel.module.less';

interface OfficialFeaturedCarouselProps {
  items: OfficialCatalogCardData[];
  /** hero：嵌在页头副标题与搜索之间，隐藏区块标题 */
  placement?: 'hero' | 'footer';
  onExperience: (item: OfficialCatalogCardData) => void;
  experiencing?: boolean;
}

function OfficialFeaturedCarousel({
  items,
  placement = 'footer',
  onExperience,
  experiencing,
}: OfficialFeaturedCarouselProps) {
  const navigate = useNavigate();
  const [active, setActive] = useState(0);
  const timerRef = useRef<number | null>(null);

  const count = items.length;
  const safeActive = count > 0 ? active % count : 0;
  const current = items[safeActive];

  const go = useCallback(
    (next: number) => {
      if (count === 0) return;
      setActive((next + count) % count);
    },
    [count],
  );

  const resetTimer = useCallback(() => {
    if (timerRef.current != null) window.clearInterval(timerRef.current);
    if (count <= 1) return;
    timerRef.current = window.setInterval(() => {
      setActive((prev) => (prev + 1) % count);
    }, FEATURED_AUTOPLAY_MS);
  }, [count]);

  useEffect(() => {
    resetTimer();
    return () => {
      if (timerRef.current != null) window.clearInterval(timerRef.current);
    };
  }, [resetTimer]);

  const openDetail = (item: OfficialCatalogCardData) => {
    navigate(buildOfficialMarketPath(item));
  };

  if (count === 0 || !current) return null;

  const isHero = placement === 'hero';

  return (
    <section
      className={`${styles.section} ${isHero ? styles.sectionHero : ''}`}
      aria-label={OFFICIAL_GALLERY_TEXT.featuredLabel}
      style={{ '--carousel-fade-ms': `${FEATURED_FADE_MS}ms` } as CSSProperties}
    >
      {!isHero && (
        <header className={styles.head}>
          <div className={styles.headTitleRow}>
            <span className={styles.headStar} aria-hidden>
              ✦
            </span>
            <h3 className={styles.headTitle}>{OFFICIAL_GALLERY_TEXT.featuredLabel}</h3>
          </div>
          <p className={styles.headDesc}>官方力荐 · 先看落地页，再一键体验</p>
        </header>
      )}

      <div
        className={styles.frame}
        onMouseEnter={() => {
          if (timerRef.current != null) window.clearInterval(timerRef.current);
        }}
        onMouseLeave={resetTimer}
      >
        {count > 1 && (
          <>
            <button
              type="button"
              className={`${styles.navBtn} ${styles.navPrev}`}
              aria-label="上一项"
              onClick={() => go(safeActive - 1)}
            >
              <LeftOutlined />
            </button>
            <button
              type="button"
              className={`${styles.navBtn} ${styles.navNext}`}
              aria-label="下一项"
              onClick={() => go(safeActive + 1)}
            >
              <RightOutlined />
            </button>
          </>
        )}

        <div className={styles.viewport}>
          {items.map((item, index) => {
            const slideMeta = buildFeaturedSlideMeta(item);
            const slideTheme = officialCoverThemeIndex(item.key);
            const isActive = index === safeActive;
            return (
              <article
                key={item.key}
                className={`${styles.slide} ${isActive ? styles.slideActive : ''} ${styles[`slideTheme${slideTheme}` as keyof typeof styles]}`}
                aria-hidden={!isActive}
              >
                <div
                  className={styles.slidePhoto}
                  style={{ backgroundImage: `url(${FEATURED_SLIDE_BACKGROUNDS[slideTheme]})` }}
                  aria-hidden
                />
                <div className={styles.slideInner}>
                  <div className={styles.copy}>
                    <span className={styles.eyebrow}>
                      {item.variant === 'workflow' ? (
                        <ApartmentOutlined className={styles.eyebrowIcon} />
                      ) : (
                        <AgentMarkIcon size={14} />
                      )}
                      {slideMeta.eyebrow}
                    </span>
                    <h4 className={styles.title}>{item.name}</h4>
                    <p className={styles.hook}>{slideMeta.hook}</p>
                    <p className={styles.pitch}>{slideMeta.pitch}</p>
                    <div className={styles.stats}>
                      {slideMeta.stats.map((stat) => (
                        <div key={`${item.key}-${stat.label}`} className={styles.stat}>
                          <strong className={styles.statValue}>{stat.value}</strong>
                          <span className={styles.statLabel}>{stat.label}</span>
                        </div>
                      ))}
                    </div>
                    <div className={styles.actions}>
                      <Button
                        type="primary"
                        size="large"
                        className={styles.cta}
                        loading={!!experiencing && item.variant === 'planning' && item.key === current.key}
                        onClick={() => onExperience(item)}
                      >
                        立即使用
                        <ArrowRightOutlined />
                      </Button>
                      {/* 工作流无详情中间页，整卡/CTA 即体验；自主规划保留详情 */}
                      {item.variant !== 'workflow' ? (
                        <Button size="large" onClick={() => openDetail(item)}>
                          查看详情
                        </Button>
                      ) : null}
                    </div>
                  </div>
                  <FeaturedAgentVisual variant={item.variant} />
                </div>
              </article>
            );
          })}
        </div>

        {count > 1 && (
          <div className={styles.dots} role="tablist" aria-label="轮播分页">
            {items.map((dotItem, index) => (
              <button
                key={dotItem.key}
                type="button"
                role="tab"
                aria-selected={index === safeActive}
                aria-label={`第 ${index + 1} 项`}
                className={`${styles.dot} ${index === safeActive ? styles.dotActive : ''}`}
                onClick={() => setActive(index)}
              />
            ))}
          </div>
        )}
      </div>
    </section>
  );
}

export default OfficialFeaturedCarousel;
