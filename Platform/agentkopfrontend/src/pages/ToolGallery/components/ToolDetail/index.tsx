/**
 * 工具详情页：运行 + 结果 + 历史
 */
import { useCallback, useRef, useState } from 'react';
import { Button, Empty, Spin, Tag } from 'antd';
import { ArrowLeftOutlined, ToolOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import type { OfficialToolItem } from '@/types/officialTools';
import type { CapabilityRunHistoryRecord } from '@/types/capabilityRuns';
import { OFFICIAL_TOOLS_QUERY_KEY, getOfficialTools } from '../../api';
import { useToolCapabilityRun } from '../../hooks/useToolCapabilityRun';
import { TOOL_GALLERY_TEXT } from '../../mock';
import { resolveCapabilityId, toToolCard } from '../../utils';
import { ToolRunForm } from './ToolRunForm';
import { ToolRunHistory } from './ToolRunHistory';
import { ToolRunResultPanel } from './ToolRunResultPanel';
import styles from '../../ToolGallery.module.less';

function ToolDetail() {
  const { toolName = '' } = useParams<{ toolName: string }>();
  const navigate = useNavigate();
  const decodedName = decodeURIComponent(toolName);
  const [historyRefreshKey, setHistoryRefreshKey] = useState(0);
  const [formPrefillToken, setFormPrefillToken] = useState(0);
  const [formPrefillArgs, setFormPrefillArgs] = useState<Record<string, unknown> | null>(null);
  const resultSectionRef = useRef<HTMLElement>(null);

  const { data, isLoading } = useQuery({
    queryKey: OFFICIAL_TOOLS_QUERY_KEY,
    queryFn: getOfficialTools,
  });

  const toolItem: OfficialToolItem | undefined = (data?.tools ?? []).find(
    (tool) => tool.name === decodedName,
  );
  const item = toolItem ? toToolCard(toolItem) : undefined;
  const capabilityId = toolItem ? resolveCapabilityId(toolItem) : '';

  const handleRunTerminal = useCallback(() => {
    setHistoryRefreshKey((k) => k + 1);
  }, []);

  const { runResult, isRunning, cancelling, loadingExecutionId, runTool, cancelRun, loadRun } =
    useToolCapabilityRun({
      capabilityId,
      onTerminal: handleRunTerminal,
    });

  const handleRun = async (args: Record<string, unknown>, title?: string) => {
    const result = await runTool(args, title);
    if (result) {
      setHistoryRefreshKey((k) => k + 1);
    }
  };

  const handleHistorySelect = (record: CapabilityRunHistoryRecord) => {
    if (record.args && Object.keys(record.args).length > 0) {
      setFormPrefillArgs(record.args);
      setFormPrefillToken(Date.now());
    }
    void loadRun(record);
    resultSectionRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  };

  if (isLoading && !item) {
    return (
      <div className={styles.detailPageLoading}>
        <Spin size="large" />
      </div>
    );
  }

  if (!item || !toolItem) {
    return (
      <div className={styles.detailPageLoading}>
        <Empty description={TOOL_GALLERY_TEXT.detailNotFound}>
          <Button type="link" onClick={() => navigate('/tools')}>
            返回工具广场
          </Button>
        </Empty>
      </div>
    );
  }

  return (
    <div className={styles.detailPage}>
      <header className={styles.detailPageHeader}>
        <Button
          type="text"
          icon={<ArrowLeftOutlined />}
          onClick={() => navigate('/tools')}
          aria-label="返回工具广场"
        />
        <div className={styles.detailIcon}>
          <ToolOutlined />
        </div>
        <div className={styles.detailIntroText}>
          <h1 className={styles.detailPageTitle}>{item.name}</h1>
          {item.description ? <p className={styles.detailDesc}>{item.description}</p> : null}
          <div className={styles.cardTags}>
            <Tag>{item.kind}</Tag>
            {item.modelSelectable ? <Tag>可选模型</Tag> : null}
            <Tag>{item.parameters.length} 个参数</Tag>
            <Tag>{item.capability}</Tag>
          </div>
        </div>
      </header>

      <div className={styles.detailRunLayout}>
        <div className={styles.detailRunMain}>
          <ToolRunForm
            tool={toolItem}
            running={isRunning}
            cancelling={cancelling}
            prefillArgs={formPrefillArgs}
            prefillToken={formPrefillToken}
            onRun={(args, title) => void handleRun(args, title)}
            onCancel={() => void cancelRun()}
          />
          <section ref={resultSectionRef} className={styles.runResultSection}>
            <h2 className={styles.paramSectionTitle}>执行结果</h2>
            <ToolRunResultPanel result={runResult} />
          </section>
        </div>

        <aside className={styles.detailRunAside}>
          <ToolRunHistory
            capabilityId={capabilityId}
            activeExecutionId={runResult?.executionId}
            loadingExecutionId={loadingExecutionId}
            liveRunResult={runResult}
            onSelect={handleHistorySelect}
            refreshKey={historyRefreshKey}
          />
        </aside>
      </div>
    </div>
  );
}

export default ToolDetail;
