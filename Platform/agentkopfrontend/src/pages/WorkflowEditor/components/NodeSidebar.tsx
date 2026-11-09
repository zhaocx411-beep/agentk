import { useCallback, useRef, type DragEvent } from 'react';
import { useReactFlow } from '@xyflow/react';
import { Tooltip } from 'antd';
import { modal } from '@/utils/feedback';
import type { AgentTemplate, WorkflowTemplate } from '../types';
import { AGENT_NODE_TYPE_CONFIG, AGENT_TEMPLATES, NODE_GROUPS } from '../constants';
import { WORKFLOW_TEMPLATES } from '../mock';
import { useWorkflowStore } from '../store';
import styles from './NodeSidebar.module.less';

/** 侧边栏 - Agent 模板拖拽面板 */
function NodeSidebar() {
  const dragRef = useRef<AgentTemplate | null>(null);
  const loadTemplate = useWorkflowStore((s) => s.loadTemplate);
  const rfInstance = useReactFlow();

  const onDragStart = useCallback((event: DragEvent<HTMLDivElement>, template: AgentTemplate) => {
    dragRef.current = template;
    event.dataTransfer.setData('application/agentflow', JSON.stringify(template));
    event.dataTransfer.effectAllowed = 'move';
  }, []);

  /** 加载快捷模板：确认后原子替换画布并自动 fitView */
  const handleLoadTemplate = useCallback(
    (template: WorkflowTemplate) => {
      modal.confirm({
        title: `加载「${template.name}」模板？`,
        content: '当前画布内容会被替换，未保存的改动将丢失。',
        okText: '加载',
        cancelText: '取消',
        onOk: () => {
          loadTemplate(template.nodes, template.edges);
          // 下一帧再 fitView，确保节点已渲染
          setTimeout(() => rfInstance.fitView({ duration: 300, padding: 0.3 }), 50);
        },
      });
    },
    [loadTemplate, rfInstance],
  );

  return (
    <div className={styles.sidebar}>
      <div className={styles.title}>节点模板</div>
      <div className={styles.hint}>拖拽节点到画布</div>

      {NODE_GROUPS.map((group) => (
        <div key={group.title} className={styles.group}>
          <div className={styles.groupTitle}>{group.title}</div>
          <div className={styles.groupHint}>{group.hint}</div>
          <div className={styles.groupList}>
            {group.types.map((type) => {
              const template = AGENT_TEMPLATES.find((t) => t.type === type);
              if (!template) return null;
              const config = AGENT_NODE_TYPE_CONFIG[type];
              return (
                <Tooltip key={type} title={template.description} placement="right">
                  <div
                    className={styles.template}
                    draggable
                    onDragStart={(e) => onDragStart(e, template)}
                    style={{ borderLeftColor: config.color }}
                  >
                    <div className={styles.icon} style={{ color: config.color }}>
                      {template.icon}
                    </div>
                    <div className={styles.info}>
                      <div className={styles.name}>{template.label}</div>
                    </div>
                  </div>
                </Tooltip>
              );
            })}
          </div>
        </div>
      ))}

      {/* 快捷 Pipeline 模板 */}
      <div className={styles.templatesDivider} />
      <div className={styles.title}>快捷模板</div>
      <div className={styles.hint}>一键加载完整 Pipeline</div>

      {WORKFLOW_TEMPLATES.map((tpl) => (
        <button
          key={tpl.id}
          type="button"
          className={styles.workflowTemplate}
          style={{ borderLeftColor: tpl.color }}
          onClick={() => handleLoadTemplate(tpl)}
        >
          <div className={styles.icon} style={{ color: tpl.color }}>
            {tpl.icon}
          </div>
          <div className={styles.info}>
            <div className={styles.name}>{tpl.name}</div>
            <div className={styles.desc}>{tpl.description}</div>
          </div>
        </button>
      ))}
    </div>
  );
}

export default NodeSidebar;
