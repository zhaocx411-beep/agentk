import { useState } from 'react';
import { Alert, Avatar, Button, Modal, Skeleton, Tag } from 'antd';
import { LeftOutlined, PictureOutlined, RightOutlined, UserOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { getPortfolioAsset, portfolioAssetKey } from '../api';
import type { PortfolioRound } from '../types';
import ImagePreview from './ImagePreview';
import { WorkConfigPanel } from './WorkConfigPanel';
import styles from './WorkDetailModal.module.less';
interface WorkDetailModalProps {
  round: PortfolioRound | null;
  hasPrev: boolean;
  hasNext: boolean;
  onPrev: () => void;
  onNext: () => void;
  onClose: () => void;
}
/** 正式作品详情弹窗，支持切换同轮素材 */
function WorkDetailModal({
  round,
  hasPrev,
  hasNext,
  onPrev,
  onNext,
  onClose,
}: WorkDetailModalProps) {
  const [activeAssetId, setActiveAssetId] = useState<number | undefined>(round?.cover.assetId);
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const assetId = activeAssetId ?? round?.cover.assetId;

  const {
    data: detail,
    isLoading,
    isError,
    refetch,
  } = useQuery({
    queryKey: portfolioAssetKey(assetId),
    queryFn: () => getPortfolioAsset(assetId as number),
    enabled: assetId !== undefined,
  });
  const agentName = detail?.agentName || round?.agentName;

  return (
    <>
      <Modal
        title={
          <span className={styles.modalTitle}>
            {agentName && <Tag>{agentName}</Tag>}
            <span>{detail?.promptText?.trim() || round?.promptText?.trim() || '未填写创作指令'}</span>
          </span>
        }
        open={!!round}
        onCancel={onClose}
        footer={
          <div className={styles.modalFooter}>
            <Button icon={<LeftOutlined />} disabled={!hasPrev} onClick={onPrev}>
              上一组
            </Button>
            <Button
              icon={<RightOutlined />}
              disabled={!hasNext}
              onClick={onNext}
              iconPosition="end"
            >
              下一组
            </Button>
          </div>
        }
        width={960}
        destroyOnHidden
      >
        {isLoading ? (
          <Skeleton active />
        ) : isError || !detail ? (
          <Alert
            type="error"
            showIcon
            message="作品详情加载失败"
            action={<Button onClick={() => refetch()}>重新加载</Button>}
          />
        ) : (
          <div className={styles.modalBody}>
            <div className={styles.topSection}>
              <div className={styles.mediaArea}>
                {detail.assetType === 'video' ? (
                  <video className={styles.videoPlayer} src={detail.assetUrl} controls />
                ) : (
                  <button
                    type="button"
                    className={styles.mainImageButton}
                    onClick={() => setPreviewUrl(detail.assetUrl)}
                  >
                    <img
                      className={styles.displayImage}
                      src={detail.assetUrl}
                      alt={detail.promptText}
                    />
                  </button>
                )}
              </div>

              <WorkConfigPanel detail={detail} />
            </div>

            {detail.sameRoundAssets.length > 0 && (
              <section className={styles.gallerySection}>
                <h4 className={styles.galleryTitle}>
                  <PictureOutlined />
                  本轮作品
                  <span className={styles.galleryCount}>{detail.sameRoundAssets.length}</span>
                </h4>
                <div className={styles.galleryGrid}>
                  {detail.sameRoundAssets.map((asset) => (
                    <button
                      type="button"
                      key={asset.assetId}
                      className={`${styles.galleryItem} ${
                        asset.assetId === detail.assetId ? styles.galleryItemActive : ''
                      }`}
                      onClick={() => setActiveAssetId(asset.assetId)}
                    >
                      {asset.assetType === 'video' ? (
                        <video className={styles.galleryImage} src={asset.assetUrl} muted />
                      ) : (
                        <img
                          className={styles.galleryImage}
                          src={asset.assetUrl}
                          alt={`同轮作品 ${asset.assetId}`}
                          loading="lazy"
                        />
                      )}
                    </button>
                  ))}
                </div>
              </section>
            )}

            <div className={styles.metaRow}>
              <Avatar size={32} icon={<UserOutlined />} className={styles.avatar}>
                {(detail.userId || '?').charAt(0)}
              </Avatar>
              <span className={styles.authorName}>{detail.userId || '未知用户'}</span>
              <Tag>{detail.status}</Tag>
              <span className={styles.updateTime}>{detail.updatedAt || detail.createdAt}</span>
            </div>
          </div>
        )}
      </Modal>

      <ImagePreview url={previewUrl} onClose={() => setPreviewUrl(null)} />
    </>
  );
}
export default WorkDetailModal;
