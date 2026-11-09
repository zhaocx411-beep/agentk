/**
 * ChatEmptyState - 无消息时的 welcome 空态
 * 品牌色点缀 + 快捷提问，引导用户开始第一轮对话
 */
import type { ReactNode } from 'react';
import { Skeleton } from 'antd';
import { AgentMarkIcon } from '@/components';
import { CHAT_EMPTY_TEXT, DEFAULT_CHAT_SUGGESTIONS } from '../../mock';
import { getSuggestionLabel, getSuggestionPrompt } from './chatSuggestionUtils';
import styles from './ChatEmptyState.module.less';

export const DEFAULT_SUGGESTION_SKELETON_COUNT = 3;

export interface ChatSuggestionItem {
  label: string;
  prompt: string;
  /** TEMPLATE 用例 referImages / extraFields（fileUrl、audioUrl 等）里的远程素材 URL */
  referUrls?: string[];
}

export type ChatSuggestion = string | ChatSuggestionItem;

interface ChatEmptyStateProps {
  agentName?: string;
  suggestions?: ChatSuggestion[];
  onSuggestion?: (text: string) => void;
  variant?: 'default' | 'quiet';
  showIcon?: boolean;
  title?: string;
  description?: string;
  /** 精选卡片下方的扩展区（如 agent-loop 模版下拉） */
  promptExtra?: ReactNode;
  /** 模版接口加载中：占位骨架，避免先展示 fallback 再闪成接口数据 */
  suggestionsLoading?: boolean;
  suggestionSkeletonCount?: number;
}

function ChatEmptyState({
  agentName,
  suggestions = DEFAULT_CHAT_SUGGESTIONS,
  onSuggestion,
  variant = 'default',
  showIcon = true,
  title,
  description,
  promptExtra,
  suggestionsLoading = false,
  suggestionSkeletonCount = DEFAULT_SUGGESTION_SKELETON_COUNT,
}: ChatEmptyStateProps) {
  const showSuggestions = onSuggestion && (suggestionsLoading || suggestions.length > 0);
  const skeletonCount = Math.max(1, suggestionSkeletonCount);

  return (
    <div className={`${styles.wrap} ${variant === 'quiet' ? styles.quiet : ''}`}>
      {showIcon ? (
        <div className={styles.mark} aria-hidden>
          <span className={styles.glow} />
          <AgentMarkIcon size={36} />
        </div>
      ) : null}
      <h3 className={styles.title}>
        {title || (agentName ? `开始与「${agentName}」对话` : CHAT_EMPTY_TEXT.title)}
      </h3>
      <p className={styles.desc}>{description || CHAT_EMPTY_TEXT.desc}</p>
      {showSuggestions ? (
        <div className={styles.chips} aria-busy={suggestionsLoading}>
          {suggestionsLoading
            ? Array.from({ length: skeletonCount }, (_, index) => (
                <Skeleton.Button
                  key={`skeleton-${index}`}
                  active
                  block
                  className={styles.chipSkeleton}
                />
              ))
            : suggestions.map((item, index) => {
                const label = getSuggestionLabel(item);
                const prompt = getSuggestionPrompt(item);
                return (
                  <button
                    key={`${label}-${index}`}
                    type="button"
                    className={styles.chip}
                    onClick={() => onSuggestion?.(prompt)}
                  >
                    {label}
                  </button>
                );
              })}
        </div>
      ) : null}
      {promptExtra ? <div className={styles.promptExtra}>{promptExtra}</div> : null}
    </div>
  );
}

export default ChatEmptyState;
