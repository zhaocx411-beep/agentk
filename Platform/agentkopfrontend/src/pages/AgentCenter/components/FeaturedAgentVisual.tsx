/**
 * FeaturedAgentVisual - 精选轮播右侧：摄影质感底图 + 玻璃态矢量图标
 */
import { FEATURED_VARIANT_ICONS } from '../lib/featuredVisuals';
import styles from './OfficialFeaturedCarousel.module.less';

interface FeaturedAgentVisualProps {
  variant: 'planning' | 'workflow';
}

/** 自主规划：水晶球 + 轨道光晕 */
function PlanningVisual() {
  return (
    <svg className={styles.visualSvg} viewBox="0 0 200 200" fill="none" aria-hidden>
      <circle cx="100" cy="100" r="72" stroke="currentColor" strokeWidth="1.2" opacity="0.18" />
      <circle cx="100" cy="100" r="54" stroke="currentColor" strokeWidth="1.5" opacity="0.28" />
      <ellipse
        cx="100"
        cy="100"
        rx="68"
        ry="24"
        stroke="currentColor"
        strokeWidth="1.4"
        opacity="0.42"
        transform="rotate(-24 100 100)"
      />
      <ellipse
        cx="100"
        cy="100"
        rx="68"
        ry="24"
        stroke="currentColor"
        strokeWidth="1.4"
        opacity="0.32"
        transform="rotate(32 100 100)"
      />
      <circle cx="100" cy="100" r="34" fill="currentColor" opacity="0.16" />
      <circle cx="100" cy="100" r="22" fill="currentColor" opacity="0.92" />
      <path
        d="M100 62c1.2 11.5 9.8 20 21.5 21.5-11.7 1.5-20.3 10-21.5 21.5-1.2-11.5-9.8-20-21.5-21.5 11.7-1.5 20.3-10 21.5-21.5Z"
        fill="currentColor"
        opacity="0.55"
      />
      <path
        d="M142 58c.6 6.2 5.4 11 11.6 11.6-6.2.6-11 5.4-11.6 11.6-.6-6.2-5.4-11-11.6-11.6 6.2-.6 11-5.4 11.6-11.6Z"
        fill="currentColor"
        opacity="0.78"
      />
      <circle cx="58" cy="132" r="4.5" fill="currentColor" opacity="0.45" />
      <circle cx="148" cy="128" r="3" fill="currentColor" opacity="0.38" />
      <circle cx="126" cy="46" r="2.5" fill="currentColor" opacity="0.5" />
    </svg>
  );
}

/** 工作流：玻璃节点 + 流光连线 */
function WorkflowVisual() {
  return (
    <svg className={styles.visualSvg} viewBox="0 0 200 200" fill="none" aria-hidden>
      <path
        d="M48 104h36M116 104h36M84 104c8-18 24-28 42-28M84 104c8 18 24 28 42 28"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        opacity="0.38"
      />
      <rect
        x="34"
        y="88"
        width="40"
        height="40"
        rx="12"
        stroke="currentColor"
        strokeWidth="2"
        opacity="0.72"
      />
      <rect x="42" y="96" width="24" height="24" rx="7" fill="currentColor" opacity="0.22" />
      <circle cx="54" cy="108" r="5" fill="currentColor" opacity="0.95" />
      <rect
        x="80"
        y="68"
        width="40"
        height="40"
        rx="12"
        stroke="currentColor"
        strokeWidth="2"
        opacity="0.88"
      />
      <rect x="88" y="76" width="24" height="24" rx="7" fill="currentColor" opacity="0.28" />
      <circle cx="100" cy="88" r="5" fill="currentColor" opacity="0.95" />
      <rect
        x="126"
        y="88"
        width="40"
        height="40"
        rx="12"
        stroke="currentColor"
        strokeWidth="2"
        opacity="0.72"
      />
      <rect x="134" y="96" width="24" height="24" rx="7" fill="currentColor" opacity="0.22" />
      <circle cx="146" cy="108" r="5" fill="currentColor" opacity="0.95" />
      <path
        d="M100 48v12M100 136v16"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        opacity="0.35"
      />
      <circle cx="100" cy="42" r="3" fill="currentColor" opacity="0.5" />
    </svg>
  );
}

function FeaturedAgentVisual({ variant }: FeaturedAgentVisualProps) {
  const photo = FEATURED_VARIANT_ICONS[variant];

  return (
    <div className={styles.visualWrap}>
      <span className={styles.visualGlow} aria-hidden />
      <div className={styles.visualCard}>
        <img
          className={styles.visualPhoto}
          src={photo}
          alt=""
          loading="lazy"
          decoding="async"
          width={640}
          height={640}
        />
        <span className={styles.visualScrim} aria-hidden />
        <span className={styles.visualBadge}>{variant === 'workflow' ? 'WORKFLOW' : 'AGENT'}</span>
        <div className={styles.visualOverlay}>
          {variant === 'workflow' ? <WorkflowVisual /> : <PlanningVisual />}
        </div>
      </div>
    </div>
  );
}

export default FeaturedAgentVisual;
