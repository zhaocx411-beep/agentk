import type { ReactNode } from 'react';
import { Typography } from 'antd';
import styles from './PageContainer.module.less';

const { Title } = Typography;

interface PageContainerProps {
  /** 页面标题 */
  title?: string;
  /** 标题右侧额外操作区 */
  extra?: ReactNode;
  /** 页面内容 */
  children?: ReactNode;
}

/**
 * PageContainer - 通用页面容器组件
 * 统一页面标题、布局结构
 *
 * @example
 * <PageContainer title="Agent列表" extra={<Button>新建</Button>}>
 *   {content}
 * </PageContainer>
 */
function PageContainer({ title, extra, children }: PageContainerProps) {
  return (
    <div className={styles.container}>
      {/* 页面头部 */}
      {(title || extra) && (
        <div className={styles.header}>
          {title && (
            <Title level={4} className={styles.title}>
              {title}
            </Title>
          )}
          {extra && <div className={styles.extra}>{extra}</div>}
        </div>
      )}

      {/* 页面内容 */}
      <div className={styles.body}>{children}</div>
    </div>
  );
}

export default PageContainer;
