import { Button, Tag } from 'antd';
import { ArrowRightOutlined, ExportOutlined, GithubOutlined } from '@ant-design/icons';
import type { OssTool } from '../types';
import styles from '../OssToolLibrary.module.less';

interface OssToolCardProps {
  tool: OssTool;
  onSelect: (tool: OssTool) => void;
}

export function OssToolCard({ tool, onSelect }: OssToolCardProps) {
  const primaryUrl = tool.repoUrl ?? tool.websiteUrl;
  const sourceLabel = tool.repoUrl ? '开源项目' : '生态产品';

  return (
    <article className={styles.card}>
      <div className={styles.cardTop}>
        <div className={styles.toolMark} aria-hidden>
          {tool.repoUrl ? <GithubOutlined /> : <ExportOutlined />}
        </div>
        <div className={styles.cardHeading}>
          <h3 className={styles.cardTitle}>{tool.name}</h3>
          <div className={styles.cardMeta}>
            <span>{tool.category}</span>
            <span aria-hidden>·</span>
            <span>{tool.form}</span>
            <Tag bordered={false} className={styles.sourceTag}>
              {sourceLabel}
            </Tag>
          </div>
        </div>
      </div>

      <p className={styles.cardSummary}>{tool.summary}</p>
      <p className={styles.cardCapabilities}>
        <span className={styles.capabilityLabel}>可借鉴能力：</span>
        {tool.capabilities}
      </p>

      <div className={styles.cardTags}>
        <Tag bordered={false}>{tool.license}</Tag>
        {tool.tags.slice(0, 2).map((tag) => (
          <Tag key={tag} bordered={false}>
            {tag}
          </Tag>
        ))}
      </div>

      <div className={styles.cardActions}>
        <Button type="link" onClick={() => onSelect(tool)} className={styles.detailButton}>
          查看能力详情 <ArrowRightOutlined />
        </Button>
        {primaryUrl ? (
          <Button
            type="text"
            href={primaryUrl}
            target="_blank"
            rel="noopener noreferrer"
            icon={<ExportOutlined />}
            aria-label={`在新窗口打开 ${tool.name}`}
          />
        ) : null}
      </div>
    </article>
  );
}
