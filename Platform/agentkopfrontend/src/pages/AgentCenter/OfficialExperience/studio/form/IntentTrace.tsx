/**
 * 模版处理详情：把「模版记忆 → 样本填充 → 最终请求载荷」这条链路摊开给用户看
 * 用户不用猜模版替他改了什么
 */
import type { CreationStudioModel } from '../hooks/useCreationStudio';
import styles from '../CreationStudio.module.less';

interface IntentTraceProps {
  model: CreationStudioModel;
}

function IntentTrace({ model }: IntentTraceProps) {
  const { spec, template, sample, filledPrompt, payloadPreview } = model;

  return (
    <div className={styles.trace}>
      <div className={styles.traceStep}>
        <span className={styles.traceIndex}>1</span>
        <div className={styles.traceBody}>
          <p className={styles.traceTitle}>模版原始 Prompt</p>
          <p className={styles.traceText}>{template.prompt}</p>
        </div>
      </div>

      <div className={styles.traceStep}>
        <span className={styles.traceIndex}>2</span>
        <div className={styles.traceBody}>
          <p className={styles.traceTitle}>Memory：{template.name}</p>
          <ul className={styles.traceList}>
            {template.memory.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
        </div>
      </div>

      <div className={styles.traceStep}>
        <span className={styles.traceIndex}>3</span>
        <div className={styles.traceBody}>
          <p className={styles.traceTitle}>样本数据填充{sample ? `：${sample.title}` : ''}</p>
          <p className={styles.traceText}>{filledPrompt}</p>
        </div>
      </div>

      <div className={styles.traceStep}>
        <span className={styles.traceIndex}>4</span>
        <div className={styles.traceBody}>
          <p className={styles.traceTitle}>
            {spec.mode === 'pipeline'
              ? '最终提交给 pipeline-runs 的 args'
              : '最终发给 /messages 的 text'}
          </p>
          <pre className={styles.tracePre}>{payloadPreview}</pre>
        </div>
      </div>
    </div>
  );
}

export default IntentTrace;
