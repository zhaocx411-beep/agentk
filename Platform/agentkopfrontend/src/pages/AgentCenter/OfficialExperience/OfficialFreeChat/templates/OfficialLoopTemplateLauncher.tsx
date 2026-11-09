/**
 * Agent-loop 灵感触发：输入框工具栏入口 + 弹窗浏览
 */
import { useState, type UIEvent } from 'react';
import { BulbOutlined } from '@ant-design/icons';
import { Button, Empty, Modal, Spin } from 'antd';
import chatStyles from '@/pages/AgentPlayground/AgentPlayground.module.less';
import type { ChatSuggestionItem } from '@/pages/AgentPlayground/components/chat/ChatEmptyState';
import {
  COMPOSER_PROMPT_STRIP_COUNT,
  type OfficialLoopPromptTemplatesModel,
} from './useOfficialLoopPromptTemplates';
import styles from './OfficialLoopTemplateLauncher.module.less';

interface OfficialLoopTemplateLauncherProps {
  catalog: OfficialLoopPromptTemplatesModel;
  onPick: (item: ChatSuggestionItem) => void;
  disabled?: boolean;
}

function OfficialLoopTemplateLauncher({
  catalog,
  onPick,
  disabled = false,
}: OfficialLoopTemplateLauncherProps) {
  const [open, setOpen] = useState(false);
  const { catalog: templateItems, total, loading, loadingMore, hasMore, loadMore, fromApi } =
    catalog;

  const showLauncher = fromApi && total > COMPOSER_PROMPT_STRIP_COUNT;

  const handleListScroll = (event: UIEvent<HTMLUListElement>) => {
    if (!hasMore || loadingMore) return;
    const target = event.currentTarget;
    if (target.scrollTop + target.clientHeight >= target.scrollHeight - 32) {
      loadMore();
    }
  };

  const handlePick = (item: ChatSuggestionItem) => {
    onPick(item);
    setOpen(false);
  };

  if (!showLauncher) return null;

  return (
    <>
      <Button
        type="text"
        disabled={disabled || loading}
        icon={<BulbOutlined />}
        aria-label="打开灵感触发"
        title="灵感触发"
        className={`${chatStyles.attachBtn} ${chatStyles.attachBtnWithLabel}`}
        onClick={() => setOpen(true)}
      >
        灵感触发
        <span className={styles.count}>{total}</span>
      </Button>
      <Modal
        title="灵感触发"
        open={open}
        footer={null}
        width={560}
        destroyOnHidden
        className={styles.modal}
        onCancel={() => setOpen(false)}
      >
        <Spin spinning={loading && templateItems.length === 0}>
          {templateItems.length > 0 ? (
            <ul className={styles.list} onScroll={handleListScroll}>
              {templateItems.map((item) => (
                <li key={item.id}>
                  <button type="button" className={styles.item} onClick={() => handlePick(item)}>
                    <span className={styles.itemTitle}>{item.label}</span>
                    {item.hint ? <span className={styles.itemHint}>{item.hint}</span> : null}
                  </button>
                </li>
              ))}
              {loadingMore ? (
                <li className={styles.listFooter}>加载更多…</li>
              ) : hasMore ? (
                <li className={styles.listFooter}>
                  已加载 {templateItems.length}/{total}，继续下滑加载更多
                </li>
              ) : null}
            </ul>
          ) : (
            <div className={styles.empty}>
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={loading ? '加载中…' : '暂无灵感模版'}
              />
            </div>
          )}
        </Spin>
      </Modal>
    </>
  );
}

export default OfficialLoopTemplateLauncher;
