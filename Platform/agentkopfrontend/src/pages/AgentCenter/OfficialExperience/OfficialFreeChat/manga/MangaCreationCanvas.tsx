import type { SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import { MangaCanvasBoard } from './MangaCanvasBoard';
import { useMangaCanvasModel } from './useMangaCanvasModel';
import styles from './MangaCreationCanvas.module.less';

const STAGE_LABEL: Record<string, string> = {
  pending: '待开始',
  active: '进行中',
  done: '已完成',
};

interface MangaCreationCanvasProps {
  sessionId: string;
  sseState: SSEState;
}

export function MangaCreationCanvas({ sessionId, sseState }: MangaCreationCanvasProps) {
  const model = useMangaCanvasModel(sessionId, sseState);
  const hasBoard = model.assets.length > 0 || model.pending.length > 0;

  return (
    <div className={styles.canvas}>
      <div className={styles.head}>
        <p className={styles.title}>创作画布</p>
      </div>
      <div className={styles.chips}>
        {model.stages.map((stage, index) => (
          <span
            key={stage.id}
            className={`${styles.chip} ${
              stage.status === 'active'
                ? styles.chipActive
                : stage.status === 'done'
                  ? styles.chipDone
                  : ''
            }`}
          >
            {String(index + 1).padStart(2, '0')} {stage.name} · {STAGE_LABEL[stage.status]}
          </span>
        ))}
      </div>
      {hasBoard ? (
        <MangaCanvasBoard model={model} />
      ) : (
        <div className={styles.boardEmpty}>
          <p className={styles.empty}>
            画布会在第一张图生成后开始生长：每个产物是一个节点，相邻工序会连线过去。
          </p>
          <p className={styles.emptyHint}>在「Agent 输出」中发送消息即可开始创作</p>
        </div>
      )}
    </div>
  );
}
