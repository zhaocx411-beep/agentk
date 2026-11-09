/**
 * AgentAvatar - 品牌渐变头像
 * 自主规划：神经网络节点；工作流：ApartmentOutlined
 */
import { ApartmentOutlined } from '@ant-design/icons';
import { AgentMarkIcon } from '@/components';
import type { AgentKind } from '../types';
import styles from '../AgentCenter.module.less';

interface AgentAvatarProps {
  size?: number;
  /** planning / builtin 用 Agent 标识；workflow 用工作流图标 */
  kind?: AgentKind;
  className?: string;
}

function AgentAvatar({ size = 48, kind = 'planning', className }: AgentAvatarProps) {
  const iconSize = Math.round(size * 0.52);
  const isWorkflow = kind === 'workflow';

  return (
    <div
      className={`${styles.avatar} ${className ?? ''}`}
      style={{
        width: size,
        height: size,
        ...(isWorkflow ? { fontSize: iconSize } : undefined),
      }}
    >
      {isWorkflow ? <ApartmentOutlined /> : <AgentMarkIcon size={iconSize} />}
    </div>
  );
}

export default AgentAvatar;
