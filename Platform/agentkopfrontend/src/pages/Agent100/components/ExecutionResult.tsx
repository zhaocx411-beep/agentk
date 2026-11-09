import { Alert, Progress } from 'antd';
import { MediaPreviewThumb } from '@/components/MediaPreview';
import { ResultDataBlock } from '@/components/ResultDataBlock';
import { RESULT_OUTPUT_PREVIEW_KEYS } from '@/components/ResultDataBlock/flatten';
import { Agent100RunStatusTag } from '../runStatus';
import { AGENT100_TEXT } from '../mock';
import { AGENT100_MEDIA_FIELDS } from '../constants';
import {
  collectPreviewMedia,
  executionArgs,
  executionOutput,
  latestProgress,
  userProgressEvents,
} from '../executionView';
import { fieldLabel } from '../utils';
import type { Agent100Execution } from '../types';
import styles from '../Agent100.module.less';

interface ExecutionResultProps {
  result: Agent100Execution | null;
}

function ExecutionResult({ result }: ExecutionResultProps) {
  if (!result) {
    return <div className={styles.runResultEmpty}>{AGENT100_TEXT.resultEmpty}</div>;
  }

  const args = executionArgs(result);
  const output = executionOutput(result);
  const previewMedia = collectPreviewMedia(result);
  const progress = result.status === 'running' ? latestProgress(result.events) : null;
  const progressEvents = userProgressEvents(result.events).slice(-8);

  return (
    <div className={styles.runResultPanel}>
      <div className={styles.runResultHead}>
        <Agent100RunStatusTag status={result.status} />
        <span className={styles.runResultMeta}>{result.executionId}</span>
      </div>

      {progress ? (
        <div className={styles.progressBlock}>
          <p className={styles.runResultBlockTitle}>{progress.message}</p>
          {progress.percent != null ? <Progress percent={Math.round(progress.percent)} size="small" /> : null}
        </div>
      ) : null}

      {progressEvents.length > 1 ? (
        <ol className={styles.progressList}>
          {progressEvents.map((event) => (
            <li key={event.id}>{event.message}</li>
          ))}
        </ol>
      ) : null}

      {result.error ? (
        <Alert type="error" showIcon message={result.error} className={styles.runResultAlert} />
      ) : null}

      {Object.keys(args).length > 0 ? (
        <ResultDataBlock
          title={AGENT100_TEXT.resultInput}
          data={args}
          mediaKeys={AGENT100_MEDIA_FIELDS}
          labelOf={fieldLabel}
        />
      ) : null}

      {output || previewMedia.length > 0 ? (
        <ResultDataBlock
          title={AGENT100_TEXT.resultOutput}
          data={output ?? {}}
          skipKeys={RESULT_OUTPUT_PREVIEW_KEYS}
          mediaKeys={AGENT100_MEDIA_FIELDS}
          labelOf={fieldLabel}
        >
          {previewMedia.length > 0 ? (
            <div className={styles.runMediaGrid}>
              {previewMedia.map((item) => (
                <MediaPreviewThumb
                  key={item.url}
                  url={item.url}
                  mediaType={item.mediaType}
                  className={styles.runMediaItem}
                />
              ))}
            </div>
          ) : null}
        </ResultDataBlock>
      ) : null}
    </div>
  );
}

export { ExecutionResult };
