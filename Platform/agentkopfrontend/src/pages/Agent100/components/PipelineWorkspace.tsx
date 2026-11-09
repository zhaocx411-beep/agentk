import { useCallback, useMemo, useRef, useState } from 'react';
import { Alert, Button, Tabs } from 'antd';
import { AGENT100_TEXT } from '../mock';
import { adaptDocumentaryPipeline, toDocumentaryV2Args } from '../documentaryV2';
import { capabilityOf, extractScriptPayload, findRenderPipelineName, prettyJson } from '../utils';
import { executionArgs, executionOutput } from '../executionView';
import { useAgent100Run } from '../useAgent100Run';
import type { Agent100Config, Agent100HistoryItem, Agent100Pipeline } from '../types';
import { ConfigOverview } from './ConfigOverview';
import { ExecutionHistory } from './ExecutionHistory';
import { ExecutionResult } from './ExecutionResult';
import { PipelineForm } from './PipelineForm';
import styles from '../Agent100.module.less';

interface PipelineWorkspaceProps {
  config: Agent100Config;
  defaultTitle?: string;
}

function PipelineWorkspace({ config, defaultTitle }: PipelineWorkspaceProps) {
  const pipelines = config.pipelines;
  const [pipelineName, setPipelineName] = useState(pipelines[0]?.name ?? '');
  const [historyRefreshKey, setHistoryRefreshKey] = useState(0);
  const [prefillArgs, setPrefillArgs] = useState<Record<string, unknown> | null>(null);
  const [prefillToken, setPrefillToken] = useState(0);
  const resultSectionRef = useRef<HTMLElement>(null);

  const rawPipeline: Agent100Pipeline | undefined = useMemo(
    () => pipelines.find((item) => item.name === pipelineName) ?? pipelines[0],
    [pipelines, pipelineName],
  );
  const pipeline: Agent100Pipeline | undefined = useMemo(
    () => (rawPipeline ? adaptDocumentaryPipeline(rawPipeline, config) : undefined),
    [rawPipeline, config],
  );
  const capability = pipeline ? capabilityOf(config, pipeline.capability) : undefined;
  const canRun = Boolean(capability?.published !== false && !config.capabilityError);
  const renderPipelineName = findRenderPipelineName(pipelines);

  const refreshHistory = useCallback(() => {
    setHistoryRefreshKey((key) => key + 1);
  }, []);

  const {
    runResult,
    isRunning,
    cancelling,
    loadingExecutionId,
    hasSession,
    startRun,
    cancelRun,
    loadRun,
    resetRun,
    startNewSession,
  } = useAgent100Run({
    agentId: config.agentId,
    pipeline: pipeline?.name ?? '',
    defaultTitle,
    onStarted: refreshHistory,
    onTerminal: refreshHistory,
  });

  const clearEcho = useCallback(() => {
    resetRun();
    setPrefillArgs(null);
    setPrefillToken(0);
  }, [resetRun]);

  const handleNewSession = () => {
    clearEcho();
    startNewSession();
    refreshHistory();
  };

  const handleHistorySelect = (record: Agent100HistoryItem) => {
    void (async () => {
      const result = await loadRun(record);
      const args = executionArgs(result);
      if (Object.keys(args).length > 0) {
        setPrefillArgs(toDocumentaryV2Args(pipelineName, args, config));
        setPrefillToken(Date.now());
      }
      resultSectionRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    })();
  };

  const handleFillScript = () => {
    const script = extractScriptPayload(executionOutput(runResult));
    if (!script || !renderPipelineName) return;
    resetRun();
    setPipelineName(renderPipelineName);
    setPrefillArgs({ script: prettyJson(script) });
    setPrefillToken(Date.now());
  };

  if (!pipeline) {
    return <Alert type="info" showIcon message={AGENT100_TEXT.noTask} />;
  }

  const showFillScript =
    Boolean(renderPipelineName) &&
    pipeline.name !== renderPipelineName &&
    runResult?.status === 'completed' &&
    Boolean(extractScriptPayload(executionOutput(runResult)));

  return (
    <>
      <ConfigOverview config={config} pipeline={pipeline} />

      <Tabs
        activeKey={pipeline.name}
        onChange={(name) => {
          setPipelineName(name);
          clearEcho();
        }}
        items={pipelines.map((item) => ({
          key: item.name,
          label: item.displayName,
        }))}
      />

      {!canRun ? (
        <Alert
          type="warning"
          showIcon
          message={AGENT100_TEXT.taskUnavailable}
          description={
            config.capabilityError ||
            (capability && !capability.published
              ? AGENT100_TEXT.taskUnpublished
              : AGENT100_TEXT.taskUnknown)
          }
        />
      ) : null}

      <div className={styles.detailRunLayout}>
        <div className={styles.detailRunMain}>
          <PipelineForm
            key={pipeline.name}
            pipeline={pipeline}
            config={config}
            running={isRunning}
            cancelling={cancelling}
            canRun={canRun}
            hasSession={hasSession}
            prefillArgs={prefillArgs}
            prefillToken={prefillToken}
            onRun={(args, title) => void startRun(args, title)}
            onCancel={() => void cancelRun()}
            onNewSession={handleNewSession}
          />
          <section ref={resultSectionRef} className={styles.runResultSection}>
            <div className={styles.resultHead}>
              <h2 className={styles.sectionTitle}>{AGENT100_TEXT.resultTitle}</h2>
              {showFillScript ? (
                <Button size="small" onClick={handleFillScript}>
                  {AGENT100_TEXT.fillScriptCta}
                </Button>
              ) : null}
            </div>
            <ExecutionResult result={runResult} />
          </section>
        </div>
        <aside className={styles.detailRunAside}>
          <ExecutionHistory
            agentId={config.agentId}
            pipeline={pipeline.name}
            activeExecutionId={runResult?.executionId}
            loadingExecutionId={loadingExecutionId}
            liveRunResult={runResult}
            onSelect={handleHistorySelect}
            refreshKey={historyRefreshKey}
          />
        </aside>
      </div>
    </>
  );
}

export { PipelineWorkspace };
