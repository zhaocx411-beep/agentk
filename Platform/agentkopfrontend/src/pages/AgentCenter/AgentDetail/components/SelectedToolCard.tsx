/**
 * SelectedToolCard - 已选工具卡片：点击即可编辑默认参数
 */
import { Tooltip } from 'antd';
import { DeleteOutlined, EditOutlined } from '@ant-design/icons';
import { ACCENT_CLASS } from '../lib/accentStyles';
import { getToolMeta } from '../lib/toolMeta';
import styles from '../AgentDetail.module.less';

interface SelectedToolCardProps {
  name: string;
  description?: string;
  kind?: string;
  /** 官方声明的参数个数 */
  paramCount?: number;
  onConfigure: () => void;
  onRemove: () => void;
}

function SelectedToolCard({
  name,
  description,
  kind,
  paramCount = 0,
  onConfigure,
  onRemove,
}: SelectedToolCardProps) {
  const meta = getToolMeta(name);
  const Icon = meta.Icon;

  return (
    <article className={styles.selectedToolCard}>
      <button type="button" className={styles.selectedToolMain} onClick={onConfigure}>
        <div className={`${styles.toolIcon} ${ACCENT_CLASS[meta.accent]}`}>
          <Icon />
        </div>
        <div className={styles.toolBody}>
          <div className={styles.toolTitleRow}>
            <span className={styles.toolName} title={name}>
              {name}
            </span>
            {kind ? <span className={styles.toolCategory}>{kind}</span> : null}
            {paramCount > 0 ? <span className={styles.softTag}>{paramCount} 个参数</span> : null}
          </div>
          <p className={styles.toolDesc}>{description?.trim() || meta.description}</p>
        </div>
      </button>
      <div className={styles.selectedToolActions}>
        <Tooltip title={paramCount > 0 ? '编辑默认参数' : '查看工具'}>
          <button
            type="button"
            className={styles.toolEditAction}
            onClick={onConfigure}
            aria-label={`${paramCount > 0 ? '编辑参数' : '查看工具'} ${name}`}
          >
            <EditOutlined />
          </button>
        </Tooltip>
        <Tooltip title="删除工具">
          <button
            type="button"
            className={styles.selectedToolRemove}
            onClick={onRemove}
            aria-label={`删除工具 ${name}`}
          >
            <DeleteOutlined />
          </button>
        </Tooltip>
      </div>
    </article>
  );
}

export { SelectedToolCard };
