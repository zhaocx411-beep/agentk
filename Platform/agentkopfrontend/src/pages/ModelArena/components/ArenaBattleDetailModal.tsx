import { Descriptions, Image, Modal, Tag, Typography } from 'antd';
import type { ArenaBattleRecord } from '../types';
import styles from '../Manage.module.less';

const STATUS_META: Record<
  ArenaBattleRecord['status'],
  { label: string; color: string }
> = {
  draft: { label: '草稿', color: 'default' },
  running: { label: '进行中', color: 'processing' },
  voting: { label: '投票中', color: 'blue' },
  completed: { label: '已完成', color: 'green' },
  invalid: { label: '已失效', color: 'red' },
};

interface ArenaBattleDetailModalProps {
  record: ArenaBattleRecord | null;
  open: boolean;
  onClose: () => void;
}

export function ArenaBattleDetailModal({ record, open, onClose }: ArenaBattleDetailModalProps) {
  if (!record) return null;

  const statusMeta = STATUS_META[record.status];
  const winnerLabel =
    record.winner === 'tie'
      ? '平局'
      : record.winner === 'A'
        ? record.modelA
        : record.winner === 'B'
          ? record.modelB
          : '--';

  return (
    <Modal
      title={`对战详情 · ${record.title}`}
      open={open}
      onCancel={onClose}
      footer={null}
      width={760}
      classNames={{ body: styles.detailModalBody }}
    >
      <Descriptions size="small" bordered column={2}>
        <Descriptions.Item label="对战 ID">{record.id}</Descriptions.Item>
        <Descriptions.Item label="状态">
          <Tag color={statusMeta.color}>{statusMeta.label}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="模型 A">{record.modelA}</Descriptions.Item>
        <Descriptions.Item label="模型 B">{record.modelB}</Descriptions.Item>
        <Descriptions.Item label="投票">{`${record.votesA} : ${record.votesB}`}</Descriptions.Item>
        <Descriptions.Item label="胜者">{winnerLabel}</Descriptions.Item>
        <Descriptions.Item label="创建人">{record.creator}</Descriptions.Item>
        <Descriptions.Item label="创建时间">{record.createdAt}</Descriptions.Item>
        <Descriptions.Item label="完成时间" span={2}>
          {record.completedAt || '--'}
        </Descriptions.Item>
      </Descriptions>

      {record.summary ? (
        <Typography.Paragraph type="secondary" className={styles.detailSummary}>
          {record.summary}
        </Typography.Paragraph>
      ) : null}

      {record.invalidReason ? (
        <Typography.Paragraph type="danger" className={styles.detailWarn}>
          失效原因：{record.invalidReason}
        </Typography.Paragraph>
      ) : null}

      <Descriptions size="small" bordered column={1} className={styles.detailBlock}>
        <Descriptions.Item label="用户问题">{record.prompt}</Descriptions.Item>
        {record.imageUrl ? (
          <Descriptions.Item label="参考图片">
            <Image
              src={record.imageUrl}
              alt=""
              className={styles.detailImage}
              preview={{ mask: false }}
            />
          </Descriptions.Item>
        ) : null}
        {record.responseA ? (
          <Descriptions.Item label={`${record.modelA} 回复`}>
            <pre className={styles.detailPre}>{record.responseA}</pre>
          </Descriptions.Item>
        ) : null}
        {record.responseB ? (
          <Descriptions.Item label={`${record.modelB} 回复`}>
            <pre className={styles.detailPre}>{record.responseB}</pre>
          </Descriptions.Item>
        ) : null}
      </Descriptions>
    </Modal>
  );
}
