/**
 * CardGridSkeleton - 列表页卡片骨架
 *
 * 用于替代内容区居中的 Spin：骨架与真实卡片同形同尺寸，
 * 数据返回后布局不跳动，加载过程中用户也能预读这一屏的信息结构。
 */
import styles from './CardGridSkeleton.module.less';

/** 卡片形态：cover=官方封面卡，info=我的 Agent 信息卡，compact=工具紧凑卡 */
export type CardSkeletonVariant = 'cover' | 'info' | 'compact';

interface CardGridSkeletonProps {
  /** 骨架卡数量，与该区块常规展示的卡片数保持一致 */
  count?: number;
  /** 卡片形态 */
  variant?: CardSkeletonVariant;
  /** 网格容器 class：由调用页传入，直接复用各自的列数与间距 */
  className?: string;
}

/** 封面卡：整幅头图 + 标题 + 描述 + 标签 */
function CoverCardSkeleton() {
  return (
    <div className={styles.coverCard}>
      <div className={styles.cover} />
      <div className={styles.body}>
        <span className={`${styles.block} ${styles.lineTitle}`} />
        <span className={`${styles.block} ${styles.line}`} />
        <span className={`${styles.block} ${styles.lineShort}`} />
        <div className={styles.tagRow}>
          <span className={`${styles.block} ${styles.tag}`} />
          <span className={`${styles.block} ${styles.tagSm}`} />
        </div>
      </div>
    </div>
  );
}

/** 信息卡：头像 + 名称 + ID + 描述 + 底部状态栏 */
function InfoCardSkeleton() {
  return (
    <div className={styles.infoCard}>
      <div className={styles.headRow}>
        <span className={`${styles.block} ${styles.avatar}`} />
        <div className={styles.headText}>
          <span className={`${styles.block} ${styles.lineTitle}`} />
          <span className={`${styles.block} ${styles.lineTiny}`} />
        </div>
      </div>
      <span className={`${styles.block} ${styles.line}`} />
      <span className={`${styles.block} ${styles.lineShort}`} />
      <div className={styles.footRow}>
        <span className={`${styles.block} ${styles.tag}`} />
        <span className={`${styles.block} ${styles.tagSm}`} />
      </div>
    </div>
  );
}

/** 紧凑卡：左侧图标 + 右侧文本块 */
function CompactCardSkeleton() {
  return (
    <div className={styles.compactCard}>
      <span className={`${styles.block} ${styles.icon}`} />
      <div className={styles.body}>
        <span className={`${styles.block} ${styles.lineTitle}`} />
        <span className={`${styles.block} ${styles.lineTiny}`} />
        <span className={`${styles.block} ${styles.lineShort}`} />
        <div className={styles.tagRow}>
          <span className={`${styles.block} ${styles.tag}`} />
          <span className={`${styles.block} ${styles.tagSm}`} />
        </div>
      </div>
    </div>
  );
}

const VARIANT_MAP: Record<CardSkeletonVariant, () => JSX.Element> = {
  cover: CoverCardSkeleton,
  info: InfoCardSkeleton,
  compact: CompactCardSkeleton,
};

function CardGridSkeleton({ count = 6, variant = 'info', className }: CardGridSkeletonProps) {
  const Card = VARIANT_MAP[variant];

  return (
    <div className={className} role="status" aria-busy aria-label="内容加载中">
      {Array.from({ length: count }, (_, i) => (
        <Card key={i} />
      ))}
    </div>
  );
}

export default CardGridSkeleton;
