/**
 * Agent-loop 右侧产出区：大预览 + 官方配置
 */
import { useEffect, useMemo, useState, type MutableRefObject } from 'react';
import { SoundOutlined } from '@ant-design/icons';
import { Empty, Image, Segmented, Spin } from 'antd';
import type { SSEActions, SSEState } from '@/pages/AgentPlayground/hooks/useSSE';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import type { ArtifactItem } from '../../studio/lib/types';
import OfficialConfigPanel from './OfficialConfigPanel';
import OfficialLoopStreamPanel from './OfficialLoopStreamPanel';
import { useOfficialSessionArtifacts } from './useOfficialSessionArtifacts';
import { resolveOfficialLoopProfile } from '../templates/promptTemplates';
import styles from '../OfficialFreeChat.module.less';

type OutputView = 'stream' | 'artifacts' | 'config';
type ArtifactKindFilter = 'all' | 'final' | 'image' | 'video' | 'audio';

const ARTIFACT_KIND_LABEL: Record<ArtifactItem['kind'], string> = {
  image: '图片',
  video: '视频',
  audio: '音频',
};

const ARTIFACT_FILTER_EMPTY: Record<Exclude<ArtifactKindFilter, 'all'>, string> = {
  final: '暂无成片',
  image: '暂无图片产物',
  video: '暂无视频产物',
  audio: '暂无音频产物',
};

function artifactKey(artifact: ArtifactItem): string {
  return `${artifact.kind}:${artifact.url}`;
}

function filterArtifacts(artifacts: ArtifactItem[], filter: ArtifactKindFilter): ArtifactItem[] {
  if (filter === 'all') return artifacts;
  if (filter === 'final') return artifacts.filter((artifact) => artifact.tier === 'final');
  return artifacts.filter((artifact) => artifact.kind === filter);
}

function countArtifacts(artifacts: ArtifactItem[], filter: ArtifactKindFilter): number {
  return filterArtifacts(artifacts, filter).length;
}

function artifactTierLabel(artifact: ArtifactItem, showFinalTag: boolean): string | null {
  if (!showFinalTag || artifact.tier !== 'final') return null;
  return '成品';
}

interface OfficialOutputStageProps {
  item: OfficialCatalogCardData | null;
  sessionId: string;
  sseState: SSEState;
  sseActions: SSEActions;
  streamScrollRef?: MutableRefObject<HTMLDivElement | null>;
  onHistoryScroll?: () => void;
  isLoadingHistory?: boolean;
  historyScrollToken?: string | null;
}

function OfficialOutputStage({
  item,
  sessionId,
  sseState,
  sseActions,
  streamScrollRef,
  onHistoryScroll,
  isLoadingHistory,
  historyScrollToken,
}: OfficialOutputStageProps) {
  const [activeView, setActiveView] = useState<OutputView>('stream');
  const [artifactFilter, setArtifactFilter] = useState<ArtifactKindFilter>('all');
  const [activeArtifactKey, setActiveArtifactKey] = useState<string | null>(null);
  const loopProfile = useMemo(() => resolveOfficialLoopProfile(item), [item]);
  const showFinalTag = loopProfile === 'manga';
  const { artifacts, loadingArtifacts, loadingMoreArtifacts } = useOfficialSessionArtifacts(
    sessionId,
    sseState,
    showFinalTag,
  );
  const filteredArtifacts = useMemo(
    () => filterArtifacts(artifacts, artifactFilter),
    [artifacts, artifactFilter],
  );
  const isBusy = sseState.isRunning || sseState.jobs.some((job) => job.status === 'submitted');
  const pendingToolCallId = sseState.pendingAskUser?.toolCallId;

  const artifactFilterOptions = useMemo(() => {
    const base = [
      { label: '全部', value: 'all' as const },
      ...(showFinalTag ? [{ label: '成品', value: 'final' as const }] : []),
      { label: '图片', value: 'image' as const },
      { label: '视频', value: 'video' as const },
      { label: '音频', value: 'audio' as const },
    ];
    return base.map((option) => {
      const count = countArtifacts(artifacts, option.value);
      return {
        label: count > 0 ? `${option.label} ${count}` : option.label,
        value: option.value,
        disabled: option.value !== 'all' && count === 0,
      };
    });
  }, [artifacts, showFinalTag]);

  useEffect(() => {
    if (!showFinalTag && artifactFilter === 'final') {
      setArtifactFilter('all');
    }
  }, [artifactFilter, showFinalTag]);

  useEffect(() => {
    if (pendingToolCallId) setActiveView('stream');
  }, [pendingToolCallId]);

  useEffect(() => {
    if (filteredArtifacts.length === 0) {
      setActiveArtifactKey(null);
      return;
    }
    if (
      !activeArtifactKey ||
      !filteredArtifacts.some((item) => artifactKey(item) === activeArtifactKey)
    ) {
      setActiveArtifactKey(artifactKey(filteredArtifacts[0]));
    }
  }, [filteredArtifacts, activeArtifactKey]);

  const activeArtifact =
    filteredArtifacts.find((artifact) => artifactKey(artifact) === activeArtifactKey) ??
    filteredArtifacts[0] ??
    null;

  return (
    <aside className={styles.outputStage}>
      <div className={styles.outputHead}>
        <Segmented<OutputView>
          block
          aria-label="产出信息"
          value={activeView}
          options={[
            {
              label: sseState.pendingAskUser
                ? '生成过程 · 待确认'
                : isBusy
                  ? '生成过程 · 进行中'
                  : '生成过程',
              value: 'stream',
            },
            {
              label: `当前产物${artifacts.length > 0 ? ` ${artifacts.length}` : ''}`,
              value: 'artifacts',
            },
            { label: '官方配置', value: 'config' },
          ]}
          onChange={setActiveView}
        />
      </div>

      {activeView === 'stream' ? (
        <div className={styles.streamStage}>
          {sseState.error ? <div className={styles.workNoticeError}>{sseState.error}</div> : null}
          <OfficialLoopStreamPanel
            sseState={sseState}
            onAnswer={sseActions.answer}
            variant="stage"
            scrollRef={streamScrollRef}
            onHistoryScroll={onHistoryScroll}
            isLoadingHistory={isLoadingHistory}
            historyScrollToken={historyScrollToken}
          />
        </div>
      ) : null}

      {activeView === 'config' ? <OfficialConfigPanel item={item} sseState={sseState} /> : null}

      {activeView === 'artifacts' ? (
        <div className={styles.artifactStage}>
          {artifacts.length > 0 ? (
            <>
              <div className={styles.artifactFilterBar}>
                <Segmented<ArtifactKindFilter>
                  className={styles.artifactFilter}
                  value={artifactFilter}
                  options={artifactFilterOptions}
                  onChange={setArtifactFilter}
                  aria-label="产物类型筛选"
                />
                {isBusy ? (
                  <span className={styles.artifactLoadingHint}>
                    <Spin size="small" />
                    正在生成新的产物…
                  </span>
                ) : loadingMoreArtifacts ? (
                  <span className={styles.artifactLoadingHint}>
                    <Spin size="small" />
                    正在补全历史产物…
                  </span>
                ) : null}
              </div>
              {activeArtifact ? (
                <>
                  <div className={styles.artifactHero}>
                    <ArtifactPreview
                      artifact={activeArtifact}
                      featured
                      showFinalTag={showFinalTag}
                    />
                  </div>
                  {filteredArtifacts.length > 1 ? (
                    <ul className={styles.artifactThumbRow}>
                      {filteredArtifacts.map((artifact) => {
                        const key = artifactKey(artifact);
                        const selected = key === activeArtifactKey;
                        return (
                          <li key={key} className={styles.artifactThumbItem}>
                            <button
                              type="button"
                              className={`${styles.artifactThumbBtn} ${
                                selected ? styles.artifactThumbBtnActive : ''
                              }`}
                              aria-label={`预览${ARTIFACT_KIND_LABEL[artifact.kind]}`}
                              aria-pressed={selected}
                              onClick={() => setActiveArtifactKey(key)}
                            >
                              <ArtifactPreview
                                artifact={artifact}
                                compact
                                showFinalTag={showFinalTag}
                              />
                            </button>
                          </li>
                        );
                      })}
                    </ul>
                  ) : null}
                </>
              ) : (
                <div className={styles.artifactEmpty}>
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description={
                      artifactFilter === 'all' ? '暂无产物' : ARTIFACT_FILTER_EMPTY[artifactFilter]
                    }
                  />
                </div>
              )}
            </>
          ) : isBusy ? (
            <div className={styles.artifactGenerating}>
              <Spin size="small" />
              <p>正在生成，完成后会自动显示在这里</p>
            </div>
          ) : loadingArtifacts ? (
            <div className={styles.artifactGenerating}>
              <Spin size="small" />
              <p>正在加载历史产物…</p>
            </div>
          ) : (
            <div className={styles.artifactEmpty}>
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="生成的图片、视频与音频会显示在这里"
              />
            </div>
          )}
        </div>
      ) : null}
    </aside>
  );
}

function ArtifactPreview({
  artifact,
  featured = false,
  compact = false,
  showFinalTag = false,
}: {
  artifact: ArtifactItem;
  featured?: boolean;
  compact?: boolean;
  showFinalTag?: boolean;
}) {
  const finalLabel = artifactTierLabel(artifact, showFinalTag);
  return (
    <figure
      className={`${styles.artifactPreview} ${featured ? styles.artifactPreviewFeatured : ''} ${
        compact ? styles.artifactPreviewCompact : ''
      }`}
    >
      {artifact.kind === 'video' ? (
        <video controls={featured} preload="metadata" src={artifact.url} />
      ) : artifact.kind === 'audio' ? (
        compact ? (
          <div className={styles.artifactAudioThumb} aria-hidden>
            <SoundOutlined className={styles.artifactAudioThumbIcon} />
          </div>
        ) : (
          <div className={styles.artifactAudioWrap}>
            <audio controls preload="metadata" src={artifact.url} />
          </div>
        )
      ) : (
        <Image src={artifact.url} alt="生成的图片" preview={featured ? { mask: false } : false} />
      )}
      {!compact ? (
        <figcaption className={styles.artifactMeta}>
          <span className={styles.artifactMetaTags}>
            {finalLabel ? <span className={styles.artifactFinalBadge}>{finalLabel}</span> : null}
            <span>{ARTIFACT_KIND_LABEL[artifact.kind]}</span>
          </span>
          <span title={artifact.from}>{artifact.from}</span>
        </figcaption>
      ) : (
        <span
          className={`${styles.artifactThumbBadge} ${
            showFinalTag && artifact.tier === 'final' ? styles.artifactThumbBadgeFinal : ''
          }`}
        >
          {finalLabel ?? ARTIFACT_KIND_LABEL[artifact.kind]}
        </span>
      )}
    </figure>
  );
}

export default OfficialOutputStage;
