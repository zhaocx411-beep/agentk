import { useCallback, useState } from 'react';
import { Input, Modal } from 'antd';
import { updateSessionTitle } from '@/pages/AgentPlayground/api';
import { message } from '@/utils/feedback';
import type { SessionTitleRenameTarget } from './types';
import styles from './SessionTitleRename.module.less';

export function useSessionTitleRename(
  onUpdated?: (sessionId: string, title: string) => void,
) {
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState<SessionTitleRenameTarget | null>(null);
  const [draft, setDraft] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const openRename = useCallback((session: SessionTitleRenameTarget) => {
    if (!session.id.startsWith('ses_')) {
      message.warning('该会话暂不支持修改标题');
      return;
    }
    setTarget(session);
    setDraft(session.title?.trim() ?? '');
    setOpen(true);
  }, []);

  const closeRename = useCallback(() => {
    if (submitting) return;
    setOpen(false);
    setTarget(null);
  }, [submitting]);

  const submitRename = useCallback(async () => {
    if (!target) return;
    const trimmed = draft.trim();
    if (!trimmed) {
      message.warning('标题不能为空');
      return;
    }
    if (trimmed === (target.title?.trim() ?? '')) {
      closeRename();
      return;
    }
    setSubmitting(true);
    try {
      const updated = await updateSessionTitle(target.id, trimmed);
      onUpdated?.(updated.id, updated.title);
      message.success('标题已更新');
      setOpen(false);
      setTarget(null);
    } catch (error) {
      message.error(error instanceof Error ? error.message : '修改标题失败');
    } finally {
      setSubmitting(false);
    }
  }, [closeRename, draft, onUpdated, target]);

  const renameModal = (
    <Modal
      title="修改对话标题"
      open={open}
      destroyOnHidden
      okText="保存"
      cancelText="取消"
      confirmLoading={submitting}
      onCancel={closeRename}
      onOk={() => void submitRename()}
    >
      <Input
        className={styles.renameModalInput}
        value={draft}
        maxLength={100}
        showCount
        placeholder="输入对话标题"
        autoFocus
        onChange={(event) => setDraft(event.target.value)}
        onPressEnter={() => void submitRename()}
      />
    </Modal>
  );

  return { openRename, renameModal };
}
