import {
  useMemo,
  type ComponentPropsWithoutRef,
  type MutableRefObject,
  type ReactNode,
} from 'react';
import { Button, Image, Spin } from 'antd';
import {
  CopyOutlined,
  DownOutlined,
  RobotOutlined,
  ThunderboltOutlined,
  ToolOutlined,
  UserOutlined,
  WarningOutlined,
} from '@ant-design/icons';
import { collectMediaUrls } from '@/pages/AgentPlayground/hooks/sse/helpers';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  restoreLegacyHeadingIcons,
  safeMarkdownUrl,
} from '@/utils/assistantMarkdown';
import { copyTextToClipboard } from '@/utils/clipboard';
import { message } from '@/utils/feedback';
import type {
  ReviewMessageItem,
  ReviewMessageRole,
} from '../conversationUtils';
import styles from '../SessionReview.module.less';

const MARKDOWN_COMPONENTS = {
  a: ({ href, children, ...props }: ComponentPropsWithoutRef<'a'>) => (
    <a href={href} target="_blank" rel="noreferrer" {...props}>
      {children}
    </a>
  ),
  table: ({ children, ...props }: ComponentPropsWithoutRef<'table'>) => (
    <div className={styles.chatMarkdownTable}>
      <table {...props}>{children}</table>
    </div>
  ),
  img: ({ src, alt }: ComponentPropsWithoutRef<'img'>) => (
    <Image
      src={src}
      alt={alt || '对话图片'}
      rootClassName={styles.chatMarkdownMedia}
      className={styles.chatMarkdownImage}
      preview={{ mask: '预览' }}
    />
  ),
};

const ROLE_META: Record<
  ReviewMessageRole,
  { label: string; rowClass: string; avatarClass: string; icon: ReactNode }
> = {
  user: {
    label: '用户',
    rowClass: styles.chatRowUser,
    avatarClass: styles.avatarUser,
    icon: <UserOutlined />,
  },
  assistant: {
    label: '助手',
    rowClass: styles.chatRowAssistant,
    avatarClass: styles.avatarAssistant,
    icon: <RobotOutlined />,
  },
  tool: {
    label: '工具',
    rowClass: styles.chatRowTool,
    avatarClass: styles.avatarTool,
    icon: <ToolOutlined />,
  },
  system: {
    label: '系统',
    rowClass: styles.chatRowSystem,
    avatarClass: styles.avatarSystem,
    icon: <WarningOutlined />,
  },
};

function ReviewMediaList({ source }: { source: unknown }) {
  const media = useMemo(() => collectMediaUrls(source), [source]);
  if (media.videos.length === 0 && media.audios.length === 0 && media.images.length === 0) {
    return null;
  }
  return (
    <div className={styles.mediaList}>
      {media.videos.map((url) => (
        <video key={url} className={styles.mediaVideo} controls preload="metadata" src={url} />
      ))}
      {media.audios.map((url) => (
        <audio key={url} className={styles.mediaAudio} controls preload="metadata" src={url} />
      ))}
      {media.images.map((url) => (
        <Image key={url} className={styles.mediaImg} src={url} alt="" preview={{ mask: false }} />
      ))}
    </div>
  );
}

function AssistantMarkdown({ content }: { content: string }) {
  return (
    <div className={styles.chatText}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={MARKDOWN_COMPONENTS}
        urlTransform={safeMarkdownUrl}
      >
        {restoreLegacyHeadingIcons(content)}
      </ReactMarkdown>
    </div>
  );
}

function ChatMessage({ item }: { item: ReviewMessageItem }) {
  const meta = ROLE_META[item.role];
  const isUser = item.role === 'user';
  const isReasoning = item.role === 'assistant' && item.kind === '思考';
  const handleCopy = async () => {
    try {
      const content =
        item.role === 'assistant'
          ? restoreLegacyHeadingIcons(item.content)
          : item.content;
      await copyTextToClipboard(content);
      message.success('已复制');
    } catch {
      message.error('复制失败');
    }
  };
  const avatar = (
    <div className={`${styles.chatAvatar} ${meta.avatarClass}`} aria-hidden>
      {meta.icon}
    </div>
  );

  return (
    <div className={`${styles.chatRow} ${meta.rowClass}`}>
      {!isUser ? avatar : null}
      <div className={styles.chatBubble}>
        <div className={styles.chatHead}>
          <span className={styles.chatRole}>{item.title || meta.label}</span>
          {item.kind !== item.title ? <span className={styles.chatKind}>{item.kind}</span> : null}
          {item.createdAt ? <span className={styles.chatTime}>{item.createdAt}</span> : null}
          <Button
            type="text"
            size="small"
            className={styles.chatCopyButton}
            icon={<CopyOutlined />}
            aria-label={`复制${item.title || meta.label}`}
            onClick={() => void handleCopy()}
          >
            复制
          </Button>
        </div>
        {item.content ? (
          isReasoning ? (
            <details className={styles.reviewReasoning}>
              <summary className={styles.reviewReasoningSummary}>
                <ThunderboltOutlined className={styles.reviewReasoningIcon} />
                <span>查看思考过程</span>
                <DownOutlined className={styles.reviewReasoningChevron} />
              </summary>
              <div className={styles.reviewReasoningBody}>
                <AssistantMarkdown content={item.content} />
              </div>
            </details>
          ) : item.role === 'assistant' ? (
            <AssistantMarkdown content={item.content} />
          ) : (
            <div className={`${styles.chatText} ${styles.chatTextPlain}`}>
              {item.content}
            </div>
          )
        ) : null}
        {item.mediaSource != null ? <ReviewMediaList source={item.mediaSource} /> : null}
      </div>
      {isUser ? avatar : null}
    </div>
  );
}

interface ConversationMessagesProps {
  messages: ReviewMessageItem[];
  loadingOlder: boolean;
  isWorkflow: boolean;
  threadRef: MutableRefObject<HTMLDivElement | null>;
  onScroll: (container: HTMLDivElement) => void;
}

export function ConversationMessages({
  messages,
  loadingOlder,
  isWorkflow,
  threadRef,
  onScroll,
}: ConversationMessagesProps) {
  return (
    <div
      ref={threadRef}
      className={styles.chatThread}
      onScroll={(event) => {
        if (!isWorkflow) onScroll(event.currentTarget);
      }}
    >
      {loadingOlder ? (
        <div className={styles.chatHistoryLoading}>
          <Spin size="small" />
          <span className={styles.loadingText}>加载更早对话…</span>
        </div>
      ) : null}
      {messages.map((item) => (
        <ChatMessage key={item.key} item={item} />
      ))}
    </div>
  );
}
