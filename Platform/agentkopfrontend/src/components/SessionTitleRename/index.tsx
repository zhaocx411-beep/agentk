/**
 * 历史会话标题修改：hover 显示编辑按钮 + 弹窗保存
 */
import type { ReactNode } from 'react';
import { EditOutlined } from '@ant-design/icons';
import { Button } from 'antd';
import type { SessionTitleRenameTarget } from './types';
import styles from './SessionTitleRename.module.less';

export type { SessionTitleRenameTarget } from './types';

interface SessionHistoryRowProps {
  className?: string;
  children: ReactNode;
}

export function SessionHistoryRow({ className, children }: SessionHistoryRowProps) {
  return <div className={`${styles.renameRow} ${className ?? ''}`}>{children}</div>;
}

interface SessionTitleRenameButtonProps {
  session: SessionTitleRenameTarget;
  onRename: (session: SessionTitleRenameTarget) => void;
  className?: string;
}

export function SessionTitleRenameButton({
  session,
  onRename,
  className,
}: SessionTitleRenameButtonProps) {
  if (!session.id.startsWith('ses_')) return null;

  return (
    <Button
      type="text"
      size="small"
      className={`${styles.renameBtn} ${className ?? ''}`}
      icon={<EditOutlined />}
      aria-label="修改标题"
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onRename(session);
      }}
    />
  );
}
