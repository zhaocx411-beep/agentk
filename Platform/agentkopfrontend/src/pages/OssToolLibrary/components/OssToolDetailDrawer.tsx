import { Alert, Button, Descriptions, Drawer, Space, Tag } from 'antd';
import { ExportOutlined, GithubOutlined } from '@ant-design/icons';
import { useResponsive } from '@/hooks';
import type { OssTool } from '../types';
import styles from '../OssToolLibrary.module.less';

interface OssToolDetailDrawerProps {
  tool?: OssTool;
  open: boolean;
  onClose: () => void;
}

export function OssToolDetailDrawer({ tool, open, onClose }: OssToolDetailDrawerProps) {
  const { isMobile } = useResponsive();
  const sourceLabel = tool?.repoUrl ? '开源项目' : '生态产品';

  return (
    <Drawer
      title={tool?.name ?? '能力详情'}
      open={open}
      onClose={onClose}
      width={isMobile ? '100%' : 520}
      className={styles.detailDrawer}
    >
      {tool ? (
        <div className={styles.drawerContent}>
          <div className={styles.drawerTags}>
            <Tag color="blue">{tool.category}</Tag>
            <Tag>{tool.form}</Tag>
            <Tag>{tool.maintenance}</Tag>
          </div>

          <p className={styles.drawerSummary}>{tool.summary}</p>

          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="来源类型">{sourceLabel}</Descriptions.Item>
            <Descriptions.Item label="可借鉴能力">{tool.capabilities}</Descriptions.Item>
            <Descriptions.Item label="许可证">{tool.license}</Descriptions.Item>
            <Descriptions.Item label="使用方式">{tool.usage}</Descriptions.Item>
          </Descriptions>

          <Alert type="warning" showIcon message="使用前请确认" description={tool.risk} />

          <section className={styles.tagSection}>
            <h4>相关标签</h4>
            <Space size={[4, 8]} wrap>
              {tool.tags.map((tag) => (
                <Tag key={tag}>{tag}</Tag>
              ))}
            </Space>
          </section>

          <div className={styles.drawerActions}>
            {tool.repoUrl ? (
              <Button
                type="primary"
                href={tool.repoUrl}
                target="_blank"
                rel="noopener noreferrer"
                icon={<GithubOutlined />}
              >
                前往 GitHub
              </Button>
            ) : null}
            {tool.websiteUrl ? (
              <Button
                href={tool.websiteUrl}
                target="_blank"
                rel="noopener noreferrer"
                icon={<ExportOutlined />}
              >
                访问官网
              </Button>
            ) : null}
          </div>
        </div>
      ) : null}
    </Drawer>
  );
}
