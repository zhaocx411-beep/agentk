/**
 * 工具运行结果：字段表 + 产物预览
 */
import { Alert } from 'antd';
import { MediaPreviewThumb } from '@/components/MediaPreview';
import { ResultDataBlock } from '@/components/ResultDataBlock';
import { RESULT_OUTPUT_PREVIEW_KEYS } from '@/components/ResultDataBlock/flatten';
import { formatUpdatedTime } from '@/utils/format';
import type { CapabilityRunResult } from '@/types/capabilityRuns';
import { RunStatusTag } from '../../lib/runStatus';
import styles from '../../ToolGallery.module.less';
import { collectPreviewMedia } from './previewMedia';

interface ToolRunResultProps {
  result: CapabilityRunResult | null;
}

function ToolRunResultPanel({ result }: ToolRunResultProps) {
  if (!result) {
    return <div className={styles.runResultEmpty}>填好参数并点击运行，结果会显示在这里</div>;
  }

  const execution = result.execution;
  const args = execution.args ?? {};
  const output = execution.output && typeof execution.output === 'object' ? execution.output : null;
  const previewMedia = collectPreviewMedia(execution);
  const finishedAt = formatUpdatedTime(execution.finishedAt) || execution.finishedAt;

  return (
    <div className={styles.runResultPanel}>
      <div className={styles.runResultHead}>
        <RunStatusTag status={result.status} />
        <span className={styles.runResultMeta}>{result.executionId}</span>
        {finishedAt ? <span className={styles.runResultMeta}>{finishedAt}</span> : null}
      </div>

      {execution.error ? (
        <Alert type="error" showIcon message={execution.error} className={styles.runResultAlert} />
      ) : null}

      {Object.keys(args).length > 0 ? <ResultDataBlock title="本次填写" data={args} /> : null}

      {output || previewMedia.length > 0 ? (
        <ResultDataBlock title="输出" data={output ?? {}} skipKeys={RESULT_OUTPUT_PREVIEW_KEYS}>
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

export { ToolRunResultPanel };
