import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Result, Space, Spin } from 'antd';
import { ArrowLeftOutlined, LinkOutlined, ReloadOutlined } from '@ant-design/icons';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import PageContainer from '@/components/PageContainer';
import { message } from '@/utils/feedback';
import { copyTextToClipboard } from '@/utils/clipboard';
import { getAdminSessionCase } from './api';
import {
  ConversationReviewContent,
  type ConversationReviewRecord,
} from './components/ConversationReviewContent';
import type { SessionReviewRecord } from './types';
import styles from './SessionReview.module.less';

interface ConversationRouteState {
  record?: SessionReviewRecord;
  returnTo?: string;
}

function ConversationDetail() {
  const navigate = useNavigate();
  const location = useLocation();
  const { agentkSessionId = '' } = useParams<{ agentkSessionId: string }>();
  const routeState = location.state as ConversationRouteState | null;
  const routeRecord = routeState?.record;
  const returnTo =
    routeState?.returnTo === '/session-review' ||
    routeState?.returnTo?.startsWith('/session-review?')
      ? routeState.returnTo
      : '/session-review';
  const initialRecord = useMemo<ConversationReviewRecord | null>(() => {
    if (!routeRecord || routeRecord.agentkSessionId !== agentkSessionId) return null;
    return routeRecord;
  }, [agentkSessionId, routeRecord]);
  const [record, setRecord] = useState<ConversationReviewRecord | null>(initialRecord);
  const [loading, setLoading] = useState(!initialRecord);
  const [error, setError] = useState('');
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    if (!agentkSessionId) {
      setError('缺少会话 ID');
      setLoading(false);
      return;
    }
    if (initialRecord) {
      setRecord(initialRecord);
      setError('');
      setLoading(false);
      return;
    }

    let cancelled = false;
    setLoading(true);
    setError('');
    getAdminSessionCase(agentkSessionId)
      .then((data) => {
        if (cancelled) return;
        setRecord({
          publicId: data.session.publicId || agentkSessionId,
          agentkSessionId: data.session.agentkSessionId || agentkSessionId,
          title: data.session.title,
          sessionKind: data.session.sessionKind,
          effectiveModel: data.session.effectiveModel,
        });
      })
      .catch((loadError: unknown) => {
        if (!cancelled) {
          setError(loadError instanceof Error ? loadError.message : '加载会话信息失败');
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [agentkSessionId, initialRecord, reloadKey]);

  const handleShare = async () => {
    try {
      await copyTextToClipboard(window.location.href);
      message.success('分享链接已复制');
    } catch {
      message.error('复制失败，请从浏览器地址栏复制链接');
    }
  };

  return (
    <PageContainer>
      <div className={styles.conversationPageToolbar}>
        <div className={styles.conversationPageToolbarMain}>
          <Button
            type="text"
            aria-label="返回数据回看列表"
            icon={<ArrowLeftOutlined />}
            onClick={() => navigate(returnTo)}
          >
            返回列表
          </Button>
          <h1 className={styles.conversationPageTitle}>对话回看</h1>
        </div>
        <Button
          type="primary"
          aria-label="分享链接"
          icon={<LinkOutlined />}
          onClick={() => void handleShare()}
        >
          分享链接
        </Button>
      </div>
      <div className={styles.conversationPageIntro}>
        独立回看本次会话的消息、工具调用与产物；复制当前网址即可分享。
      </div>
      {loading && !record ? (
        <div className={styles.conversationPageLoading}>
          <Spin />
          <span className={styles.loadingText}>加载会话信息…</span>
        </div>
      ) : error && !record ? (
        <Result
          status="warning"
          title="暂时无法加载该会话"
          subTitle={error}
          extra={
            <Space>
              <Button onClick={() => navigate(returnTo)}>返回列表</Button>
              <Button
                type="primary"
                icon={<ReloadOutlined />}
                onClick={() => setReloadKey((value) => value + 1)}
              >
                重试
              </Button>
            </Space>
          }
        />
      ) : record ? (
        <>
          {error ? <Alert type="warning" showIcon message={error} /> : null}
          <ConversationReviewContent record={record} />
        </>
      ) : null}
    </PageContainer>
  );
}

export default ConversationDetail;
