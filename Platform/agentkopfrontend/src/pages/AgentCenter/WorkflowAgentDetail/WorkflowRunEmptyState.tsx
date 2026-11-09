/**
 * WorkflowRunEmptyState - 工作流预览未运行时的引导空态
 */
import { ApartmentOutlined } from '@ant-design/icons';
import { WORKFLOW_RUN_GUIDE } from '../mock';
import styles from './WorkflowRunEmptyState.module.less';

interface WorkflowRunEmptyStateProps {
  workflowName?: string;
  needsPipelineSelect?: boolean;
}

function WorkflowRunEmptyState({
  workflowName,
  needsPipelineSelect = false,
}: WorkflowRunEmptyStateProps) {
  return (
    <div className={styles.wrap}>
      <div className={styles.mark} aria-hidden>
        <span className={styles.glow} />
        <ApartmentOutlined />
      </div>
      <h3 className={styles.title}>
        {workflowName ? `运行「${workflowName}」` : WORKFLOW_RUN_GUIDE.title}
      </h3>
      <p className={styles.desc}>{WORKFLOW_RUN_GUIDE.desc}</p>
      <ol className={styles.steps}>
        {needsPipelineSelect ? <li>{WORKFLOW_RUN_GUIDE.selectPipelineStep}</li> : null}
        {WORKFLOW_RUN_GUIDE.steps.map((step) => (
          <li key={step}>{step}</li>
        ))}
      </ol>
    </div>
  );
}

export default WorkflowRunEmptyState;
