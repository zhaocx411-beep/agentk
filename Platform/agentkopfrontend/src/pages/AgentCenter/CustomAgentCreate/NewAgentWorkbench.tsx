/**
 * NewAgentWorkbench - 自定义 Agent 新建页的配置台占位
 * 仅作视觉预览，真正编辑在填写名称并创建实体之后
 */
import { Button, Input, Tag } from 'antd';
import { ArrowLeftOutlined, CloudUploadOutlined, SaveOutlined } from '@ant-design/icons';
import { WorkbenchLayout, WorkbenchPane } from '@/components';
import { AGENT_STATUS_META } from '../constants';
import { CUSTOM_AGENT_CREATE_TEXT } from '../mock';
import detailStyles from '../AgentDetail/AgentDetail.module.less';
import styles from './CustomAgentCreate.module.less';

interface NewAgentWorkbenchProps {
  onBack: () => void;
}

function NewAgentWorkbench({ onBack }: NewAgentWorkbenchProps) {
  const copy = CUSTOM_AGENT_CREATE_TEXT;
  const draftMeta = AGENT_STATUS_META.draft;

  return (
    <div className={styles.workbench} aria-hidden>
      <WorkbenchLayout variant="configuration" className={detailStyles.page}>
        <WorkbenchPane
          ariaLabel={copy.rolePane}
          header={
            <div className={detailStyles.identityHeaderContent}>
              <Button type="text" icon={<ArrowLeftOutlined />} aria-label="返回" onClick={onBack} />
              <div className={detailStyles.headerTitleBox}>
                <div className={detailStyles.headerNameRow}>
                  <span className={detailStyles.headerName}>{copy.placeholderName}</span>
                  <Tag color={draftMeta.color} className={detailStyles.headerTag}>
                    {draftMeta.text}
                  </Tag>
                </div>
                <div className={detailStyles.headerSub}>{copy.placeholderDesc}</div>
              </div>
            </div>
          }
        >
          <section className={detailStyles.promptPanel}>
            <div className={detailStyles.sectionTitle}>{copy.rolePane}</div>
            <Input.TextArea
              disabled
              autoSize={{ minRows: 18, maxRows: 28 }}
              placeholder={copy.promptPlaceholder}
              className={detailStyles.promptArea}
            />
          </section>
        </WorkbenchPane>

        <WorkbenchPane
          ariaLabel={copy.configPane}
          header={
            <div className={detailStyles.configHeaderContent}>
              <div className={detailStyles.configHeaderInfo}>
                <span className={detailStyles.configHeaderTitle}>{copy.configPane}</span>
                <span className={detailStyles.versionPendingTip}>{copy.versionPending}</span>
              </div>
              <div className={detailStyles.headerRight}>
                <Button icon={<SaveOutlined />} disabled>
                  {copy.saveDraft}
                </Button>
                <Button type="primary" icon={<CloudUploadOutlined />} disabled>
                  {copy.publish}
                </Button>
              </div>
            </div>
          }
        >
          <div className={detailStyles.capabilityPanel}>
            <section className={detailStyles.section}>
              <div className={detailStyles.sectionTitle}>{copy.modelTitle}</div>
              <p className={styles.placeholderHint}>{copy.placeholderDesc}</p>
            </section>
            <section className={detailStyles.section}>
              <div className={detailStyles.sectionTitle}>{copy.capabilityTitle}</div>
              <p className={styles.placeholderHint}>{copy.placeholderDesc}</p>
            </section>
          </div>
        </WorkbenchPane>

        <WorkbenchPane
          tone="muted"
          ariaLabel={copy.previewPane}
          className={detailStyles.previewPanel}
          header={
            <div className={detailStyles.previewHeaderContent}>
              <span className={detailStyles.previewTitle}>{copy.previewPane}</span>
            </div>
          }
        >
          <p className={styles.previewEmpty}>{copy.previewEmpty}</p>
        </WorkbenchPane>
      </WorkbenchLayout>
    </div>
  );
}

export { NewAgentWorkbench };
