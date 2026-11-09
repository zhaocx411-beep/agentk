import { Alert, Tag } from 'antd';
import { AGENT100_TEXT } from '../mock';
import { fieldLabel } from '../utils';
import type { Agent100Config, Agent100Pipeline } from '../types';
import styles from '../Agent100.module.less';

interface ConfigOverviewProps {
  config: Agent100Config;
  pipeline?: Agent100Pipeline;
}

function ConfigOverview({ config, pipeline }: ConfigOverviewProps) {
  const modelEntries = Object.entries(config.models.defaults ?? {}).filter(([key]) => key !== 'tts');
  const tasks = config.pipelines.map((item) => ({
    key: item.name,
    label: item.displayName,
    active: item.name === pipeline?.name,
  }));

  return (
    <section className={styles.overview} aria-label={AGENT100_TEXT.overviewAria}>
      {config.capabilityError ? (
        <Alert
          type="error"
          showIcon
          message={AGENT100_TEXT.capabilityNotReady}
          description={config.capabilityError}
        />
      ) : null}
      {config.mediaError ? (
        <Alert
          type="warning"
          showIcon
          message={AGENT100_TEXT.mediaError}
          description={config.mediaError}
        />
      ) : null}

      <div className={styles.overviewRow}>
        <span className={styles.overviewLabel}>{AGENT100_TEXT.defaultModels}</span>
        <div className={styles.overviewTags}>
          {modelEntries.length === 0 ? (
            <span className={styles.overviewMuted}>{AGENT100_TEXT.noModels}</span>
          ) : (
            modelEntries.map(([key, value]) => (
              <Tag key={key} bordered={false}>
                {fieldLabel(key)}：{value}
              </Tag>
            ))
          )}
        </div>
      </div>

      {tasks.length > 0 ? (
        <div className={styles.overviewRow}>
          <span className={styles.overviewLabel}>{AGENT100_TEXT.availableTasks}</span>
          <div className={styles.overviewTags}>
            {tasks.map((item) => (
              <Tag
                key={item.key}
                bordered={false}
                className={item.active ? styles.capabilityActive : undefined}
              >
                {item.label}
              </Tag>
            ))}
          </div>
        </div>
      ) : null}
    </section>
  );
}

export { ConfigOverview };
