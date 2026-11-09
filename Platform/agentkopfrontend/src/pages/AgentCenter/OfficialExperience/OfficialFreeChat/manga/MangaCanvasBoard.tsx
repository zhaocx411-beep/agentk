import { useLayoutEffect, useRef, useState } from 'react';
import {
  assetNodeIds,
  collectCanvasLinks,
  pendingNodeId,
  type CanvasLink,
} from './drawMangaCanvasLinks';
import { assetKey, type MangaBoardStage, type MangaCanvasModel } from './foldMangaCanvas';
import styles from './MangaCreationCanvas.module.less';

const BOARD_COLUMNS: Array<{ id: MangaBoardStage; label: string }> = [
  { id: 'characters', label: '定妆 · 场景 · 道具' },
  { id: 'frames', label: '分镜首帧' },
  { id: 'clips', label: '镜头视频' },
  { id: 'film', label: '成片' },
];

const PENDING_TEXT: Record<MangaBoardStage, string> = {
  characters: '生成图片中…',
  frames: '合成图片中…',
  clips: '合成视频中…',
  film: '合成成片中…',
};

interface MangaCanvasBoardProps {
  model: MangaCanvasModel;
}

export function MangaCanvasBoard({ model }: MangaCanvasBoardProps) {
  const innerRef = useRef<HTMLDivElement>(null);
  const [links, setLinks] = useState<CanvasLink[]>([]);
  const [boardSize, setBoardSize] = useState({ width: 0, height: 0 });
  const idByKey = assetNodeIds(model.assets);

  useLayoutEffect(() => {
    const inner = innerRef.current;
    if (!inner) return;
    const ids = assetNodeIds(model.assets);
    const draw = () => {
      setBoardSize({ width: inner.scrollWidth, height: inner.scrollHeight });
      setLinks(collectCanvasLinks(inner, model, ids));
    };
    draw();
    const observer = new ResizeObserver(draw);
    observer.observe(inner);
    inner.querySelectorAll('[data-node]').forEach((node) => observer.observe(node));
    return () => observer.disconnect();
  }, [model]);

  const grouped: Record<MangaBoardStage, MangaCanvasModel['assets']> = {
    characters: [],
    frames: [],
    clips: [],
    film: [],
  };
  for (const asset of model.assets) grouped[asset.stage].push(asset);
  const pendingByStage: Record<MangaBoardStage, MangaCanvasModel['pending']> = {
    characters: [],
    frames: [],
    clips: [],
    film: [],
  };
  for (const item of model.pending) pendingByStage[item.stage].push(item);

  return (
    <div className={styles.board}>
      <div ref={innerRef} className={styles.inner}>
        <svg className={styles.links} width={boardSize.width} height={boardSize.height}>
          {links.map((link, index) => (
            <g key={`${link.d}-${index}`}>
              <path
                className={link.pending ? `${styles.link} ${styles.linkPending}` : styles.link}
                d={link.d}
              />
              <circle
                className={link.pending ? `${styles.linkDot} ${styles.linkDotPending}` : styles.linkDot}
                cx={link.x1}
                cy={link.y1}
                r="2.2"
              />
              <circle
                className={link.pending ? `${styles.linkDot} ${styles.linkDotPending}` : styles.linkDot}
                cx={link.x2}
                cy={link.y2}
                r="2.2"
              />
            </g>
          ))}
        </svg>
        {BOARD_COLUMNS.map((column) => {
          const items = grouped[column.id];
          const waiting = pendingByStage[column.id];
          return (
            <div key={column.id} className={styles.col}>
              <div className={styles.colTitle}>{column.label}</div>
              {items.map((asset) => (
                <article
                  key={asset.uri}
                  data-node={idByKey.get(assetKey(asset.uri))}
                  className={`${styles.node} ${asset.kind === 'video' ? styles.nodeVideo : ''}`}
                >
                  {asset.kind === 'video' ? (
                    <div className={styles.media}>
                      <video controls preload="metadata" src={asset.uri} />
                    </div>
                  ) : (
                    <a className={styles.media} href={asset.uri} target="_blank" rel="noreferrer">
                      <img src={asset.uri} alt="" />
                    </a>
                  )}
                  <p className={styles.caption}>{asset.description}</p>
                </article>
              ))}
              {waiting.map((item) => (
                <div key={item.id} data-node={pendingNodeId(item.id)} className={styles.generating}>
                  {PENDING_TEXT[item.stage]}
                </div>
              ))}
              {items.length === 0 && waiting.length === 0 ? (
                <div className={styles.blank}>待生成</div>
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}
