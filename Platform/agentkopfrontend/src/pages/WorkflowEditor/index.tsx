import { useCallback, useEffect, useRef, type DragEvent } from 'react';
import {
  ReactFlow,
  ReactFlowProvider,
  Controls,
  MiniMap,
  Background,
  BackgroundVariant,
  Panel,
  useReactFlow,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { Button, Space, Popconfirm, Typography, Tooltip, Dropdown } from 'antd';
import { message } from '@/utils/feedback';
import {
  ClearOutlined,
  DownloadOutlined,
  ZoomInOutlined,
  ZoomOutOutlined,
  ExpandOutlined,
  UndoOutlined,
  RedoOutlined,
  CopyOutlined,
  DeleteOutlined,
  SettingOutlined,
} from '@ant-design/icons';
import { AgentNodeComponentMemo } from './components/AgentNode';
import { AgentEdgeComponentMemo } from './components/AgentEdge';
import NodeConfigPanel from './components/NodeConfigPanel';
import NodeSidebar from './components/NodeSidebar';
import AgentKConfigDrawer from './components/AgentKConfigDrawer';
import WorkflowPanel from './components/WorkflowPanel';
import { useWorkflowStore } from './store';
import type { AgentTemplate, AgentNodeData, AgentNode, AgentEdge } from './types';
import styles from './WorkflowEditor.module.less';

const { Text } = Typography;

const edgeTypes = { agentEdge: AgentEdgeComponentMemo };

const defaultEdgeOptions = { type: 'agentEdge' as const };

/** 带右键菜单的节点包装器 */
function NodeWithContextMenu(props: React.ComponentProps<typeof AgentNodeComponentMemo>) {
  const copyNode = useWorkflowStore((s) => s.copyNode);
  const pasteNode = useWorkflowStore((s) => s.pasteNode);
  const deleteNode = useWorkflowStore((s) => s.deleteNode);
  const clipboard = useWorkflowStore((s) => s.clipboard);
  const rfInstance = useReactFlow();

  // 记录右键点击的屏幕坐标
  const contextMenuPos = useRef<{ clientX: number; clientY: number } | null>(null);

  // 右键菜单操作后统一清空选中，防止菜单关闭的 click 冒泡打开配置面板
  const suppressSelectAfterAction = useCallback(() => {
    requestAnimationFrame(() => {
      useWorkflowStore.setState({ selectedNodeId: null });
    });
  }, []);

  const items = [
    // 开始/结束节点不允许复制
    ...(props.data?.type !== 'start' && props.data?.type !== 'end'
      ? [
          {
            key: 'copy',
            icon: <CopyOutlined />,
            label: '复制节点',
            onClick: () => {
              if (props.id) copyNode(props.id);
              suppressSelectAfterAction();
            },
          },
        ]
      : []),
    {
      key: 'paste',
      icon: <CopyOutlined />,
      label: '粘贴节点',
      disabled: !clipboard,
      onClick: () => {
        if (clipboard && contextMenuPos.current) {
          const NODE_HALF_W = 90;
          const NODE_HALF_H = 34;
          const pos = rfInstance.screenToFlowPosition({
            x: contextMenuPos.current.clientX,
            y: contextMenuPos.current.clientY,
          });
          pasteNode({ x: pos.x - NODE_HALF_W, y: pos.y - NODE_HALF_H });
        } else {
          pasteNode();
        }
        suppressSelectAfterAction();
      },
    },
    // 开始/结束节点不允许删除
    ...(props.data?.type !== 'start' && props.data?.type !== 'end'
      ? [
          { type: 'divider' as const },
          {
            key: 'delete',
            icon: <DeleteOutlined />,
            label: '删除节点',
            danger: true,
            onClick: () => {
              if (props.id) deleteNode(props.id);
              suppressSelectAfterAction();
            },
          },
        ]
      : []),
  ];

  return (
    <Dropdown menu={{ items }} trigger={['contextMenu']}>
      <div
        onContextMenu={(e) => {
          // 记录右键位置，供粘贴时定位
          contextMenuPos.current = { clientX: e.clientX, clientY: e.clientY };
          // 右键菜单不触发节点选中/多选
          e.stopPropagation();
        }}
      >
        <AgentNodeComponentMemo {...props} />
      </div>
    </Dropdown>
  );
}

const nodeTypesWithMenu = { agent: NodeWithContextMenu };

function WorkflowEditorInner() {
  const reactFlowWrapper = useRef<HTMLDivElement>(null);
  const rfInstance = useReactFlow();

  const nodes = useWorkflowStore((s) => s.nodes);
  const edges = useWorkflowStore((s) => s.edges);
  const onNodesChange = useWorkflowStore((s) => s.onNodesChange);
  const onEdgesChange = useWorkflowStore((s) => s.onEdgesChange);
  const onConnect = useWorkflowStore((s) => s.onConnect);
  const addAgentNode = useWorkflowStore((s) => s.addAgentNode);
  const setSelectedNode = useWorkflowStore((s) => s.setSelectedNode);
  const clearCanvas = useWorkflowStore((s) => s.clearCanvas);
  const exportWorkflow = useWorkflowStore((s) => s.exportWorkflow);
  const undo = useWorkflowStore((s) => s.undo);
  const redo = useWorkflowStore((s) => s.redo);
  const canUndo = useWorkflowStore((s) => s.canUndo);
  const canRedo = useWorkflowStore((s) => s.canRedo);
  const copyNode = useWorkflowStore((s) => s.copyNode);
  const pasteNode = useWorkflowStore((s) => s.pasteNode);
  const deleteNode = useWorkflowStore((s) => s.deleteNode);

  const setAgentKDrawerOpen = useCallback((open: boolean) => {
    useWorkflowStore.setState({ agentKDrawerOpen: open });
  }, []);

  const agentKDrawerOpen = useWorkflowStore((s) => s.agentKDrawerOpen);

  // 消费跨页面传来的配置（Agent 调试台 → Pipeline 编辑器）
  useEffect(() => {
    import('@/store/agentConfig').then(({ useAgentConfigStore: store }) => {
      const config = store.getState().consumePendingConfig();
      if (!config || config.source !== 'agent-playground') return;

      // 从调试台配置生成 Pipeline 节点
      const newNodes: AgentNode[] = [];

      // 开始节点
      newNodes.push({
        id: 'node-start',
        type: 'agent',
        position: { x: 0, y: 200 },
        data: { label: '开始', type: 'start', status: 'idle' },
      });

      // LLM 节点
      if (config.model || config.systemPrompt) {
        newNodes.push({
          id: 'node-llm',
          type: 'agent',
          position: { x: 300, y: 200 },
          data: {
            label: config.model || 'LLM',
            type: 'llm',
            model: config.model,
            prompt: config.systemPrompt,
            status: 'idle',
          },
        });
      }

      // Tool 节点
      config.tools.forEach((toolName, idx) => {
        newNodes.push({
          id: `node-tool-${idx}`,
          type: 'agent',
          position: { x: 600, y: 100 + idx * 120 },
          data: { label: toolName, type: 'tool', toolNames: [toolName], status: 'idle' },
        });
      });

      // 结束节点
      newNodes.push({
        id: 'node-end',
        type: 'agent',
        position: { x: 900, y: 200 },
        data: { label: '结束', type: 'end', status: 'idle' },
      });

      // 连线
      const newEdges: AgentEdge[] = [];
      if (newNodes.length >= 3) {
        newEdges.push({
          id: 'e-start-llm',
          source: 'node-start',
          target: 'node-llm',
          data: { animated: true },
        });
        config.tools.forEach((_, idx) => {
          newEdges.push({
            id: `e-llm-tool-${idx}`,
            source: 'node-llm',
            target: `node-tool-${idx}`,
            data: { animated: true },
          });
          newEdges.push({
            id: `e-tool-${idx}-end`,
            source: `node-tool-${idx}`,
            target: 'node-end',
            data: {},
          });
        });
        if (config.tools.length === 0) {
          newEdges.push({
            id: 'e-llm-end',
            source: 'node-llm',
            target: 'node-end',
            data: {},
          });
        }
      }

      // 清空画布并加载新节点
      clearCanvas();
      newNodes.forEach((node) => addAgentNode(node));
      useWorkflowStore.setState({ edges: newEdges });
      message.info('已从 Agent 调试台加载配置');
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  /** 画布右键菜单位置 */
  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: { id: string }) => {
      setSelectedNode(node.id);
    },
    [setSelectedNode],
  );

  /** 画布空白处点击取消选中 */
  const onPaneClick = useCallback(() => {
    setSelectedNode(null);
  }, [setSelectedNode]);

  /** 拖拽到画布放置节点 */
  const onDragOver = useCallback((event: DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const onDrop = useCallback(
    (event: DragEvent<HTMLDivElement>) => {
      event.preventDefault();

      const dataStr = event.dataTransfer.getData('application/agentflow');
      if (!dataStr) return;

      const template: AgentTemplate = JSON.parse(dataStr);

      // 开始/结束节点全局唯一：已有则提示并移动到新位置
      if (template.type === 'start' || template.type === 'end') {
        const existing = nodes.find((n) => n.data.type === template.type);
        if (existing) {
          message.info(`${template.label} 节点已存在，已移至新位置`);
        }
      }

      // 计算放置位置：screenToFlowPosition 需要屏幕绝对坐标
      // 节点 position 是左上角锚点，偏移半个节点让鼠标对齐节点中心
      const NODE_HALF_W = 90;
      const NODE_HALF_H = 34;
      const position = rfInstance.screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      });
      position.x -= NODE_HALF_W;
      position.y -= NODE_HALF_H;

      const newNodeData: AgentNodeData = {
        label: template.defaultData.label ?? template.label,
        type: template.defaultData.type ?? template.type,
        description: template.defaultData.description ?? '',
        status: 'idle',
        model: template.defaultData.model,
        prompt: template.defaultData.prompt,
        temperature: template.defaultData.temperature,
        toolNames: template.defaultData.toolNames,
        conditionExpr: template.defaultData.conditionExpr,
        subflowId: template.defaultData.subflowId,
        outputVar: template.defaultData.outputVar,
      };

      addAgentNode({
        id: `node-${Date.now()}`,
        type: 'agent' as const,
        position,
        data: newNodeData,
      });

      message.success(`已添加 ${template.label}`);
    },
    [rfInstance, addAgentNode, nodes],
  );

  /** 节点选中 */
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // 输入框中的快捷键不拦截（允许 Ctrl+C/V 复制粘贴文字）
      const tag = (e.target as HTMLElement).tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;

      const meta = e.metaKey || e.ctrlKey;
      const selId = useWorkflowStore.getState().selectedNodeId;

      // Ctrl+Z 撤销
      if (meta && e.key === 'z' && !e.shiftKey) {
        e.preventDefault();
        undo();
        return;
      }
      // Ctrl+Shift+Z / Ctrl+Y 重做
      if ((meta && e.key === 'z' && e.shiftKey) || (meta && e.key === 'y')) {
        e.preventDefault();
        redo();
        return;
      }
      // Ctrl+C 复制节点
      if (meta && e.key === 'c' && selId) {
        copyNode(selId);
        return;
      }
      // Ctrl+V 粘贴节点
      if (meta && e.key === 'v') {
        e.preventDefault();
        pasteNode();
        requestAnimationFrame(() => {
          useWorkflowStore.setState({ selectedNodeId: null });
        });
        return;
      }
      // Delete / Backspace 删除选中节点
      if ((e.key === 'Delete' || e.key === 'Backspace') && selId) {
        deleteNode(selId);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [undo, redo, copyNode, pasteNode, deleteNode]);

  /** 画布右键菜单位置 */
  const paneContextMenuPos = useRef<{ clientX: number; clientY: number } | null>(null);

  /** 画布右键菜单：粘贴 */
  const paneContextMenuItems = useCallback(() => {
    const clipboard = useWorkflowStore.getState().clipboard;
    return [
      {
        key: 'paste',
        icon: <CopyOutlined />,
        label: '粘贴节点',
        disabled: !clipboard,
        onClick: () => {
          if (clipboard && paneContextMenuPos.current) {
            const NODE_HALF_W = 90;
            const NODE_HALF_H = 34;
            const pos = rfInstance.screenToFlowPosition({
              x: paneContextMenuPos.current.clientX,
              y: paneContextMenuPos.current.clientY,
            });
            pasteNode({ x: pos.x - NODE_HALF_W, y: pos.y - NODE_HALF_H });
          } else {
            pasteNode();
          }
          requestAnimationFrame(() => {
            useWorkflowStore.setState({ selectedNodeId: null });
          });
        },
      },
    ];
  }, [pasteNode, rfInstance]);

  const onPaneContextMenu = useCallback((e: React.MouseEvent | MouseEvent) => {
    e.preventDefault();
  }, []);

  const minimapNodeColor = useCallback((node: { data: AgentNodeData }) => {
    const colors: Record<string, string> = {
      start: '#52c41a',
      end: '#ff4d4f',
      llm: '#1677ff',
      tool: '#fa8c16',
      condition: '#722ed1',
      subflow: '#13c2c2',
    };
    return colors[node.data.type] ?? '#999';
  }, []);

  const nodeCount = nodes.length;
  const edgeCount = edges.length;

  return (
    <div className={styles.page}>
      <div className={styles.editor}>
        {/* 左侧节点模板面板 */}
        <NodeSidebar />

        {/* 画布区域 */}
        <Dropdown menu={{ items: paneContextMenuItems() }} trigger={['contextMenu']}>
          <div
            className={styles.canvas}
            ref={reactFlowWrapper}
            onContextMenu={(e) => {
              paneContextMenuPos.current = { clientX: e.clientX, clientY: e.clientY };
            }}
          >
            <ReactFlow
              nodes={nodes}
              edges={edges}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onConnect={onConnect}
              onNodeClick={onNodeClick}
              onPaneClick={onPaneClick}
              onDrop={onDrop}
              onDragOver={onDragOver}
              onPaneContextMenu={onPaneContextMenu}
              nodeTypes={nodeTypesWithMenu}
              edgeTypes={edgeTypes}
              defaultEdgeOptions={defaultEdgeOptions}
              fitView
              fitViewOptions={{ padding: 0.7 }}
              snapToGrid
              snapGrid={[15, 15]}
              deleteKeyCode={['Backspace', 'Delete']}
              proOptions={{ hideAttribution: true }}
              className={styles.reactFlow}
            >
              <Background variant={BackgroundVariant.Dots} gap={15} size={1} />
              <Controls position="bottom-left" />
              <MiniMap
                nodeColor={minimapNodeColor as never}
                maskColor="rgba(0, 0, 0, 0.08)"
                style={{ border: '1px solid #f0f0f0' }}
              />

              {/* 顶部工具栏 */}
              <Panel position="top-right" className={styles.toolbar}>
                <Space>
                  <Text type="secondary" className={styles.stats}>
                    {nodeCount} 节点 · {edgeCount} 连线
                  </Text>
                  <Tooltip title="配置模型 / 工具 / System Prompt">
                    <Button
                      type="default"
                      size="small"
                      icon={<SettingOutlined />}
                      onClick={() => setAgentKDrawerOpen(true)}
                    >
                      Agent-k 配置
                    </Button>
                  </Tooltip>
                  <Tooltip title="撤销 (Ctrl+Z)">
                    <Button
                      size="small"
                      icon={<UndoOutlined />}
                      onClick={undo}
                      disabled={!canUndo()}
                    />
                  </Tooltip>
                  <Tooltip title="重做 (Ctrl+Y)">
                    <Button
                      size="small"
                      icon={<RedoOutlined />}
                      onClick={redo}
                      disabled={!canRedo()}
                    />
                  </Tooltip>
                  <Button
                    size="small"
                    icon={<ZoomInOutlined />}
                    onClick={() => rfInstance.zoomIn()}
                  />
                  <Button
                    size="small"
                    icon={<ZoomOutOutlined />}
                    onClick={() => rfInstance.zoomOut()}
                  />
                  <Button
                    size="small"
                    icon={<ExpandOutlined />}
                    onClick={() => rfInstance.fitView()}
                  />
                  <Button
                    size="small"
                    icon={<DownloadOutlined />}
                    onClick={() => {
                      const wf = exportWorkflow();
                      const blob = new Blob([JSON.stringify(wf, null, 2)], {
                        type: 'application/json',
                      });
                      const url = URL.createObjectURL(blob);
                      const a = document.createElement('a');
                      a.href = url;
                      a.download = 'pipeline.json';
                      a.click();
                      URL.revokeObjectURL(url);
                      message.success('Pipeline 已导出');
                    }}
                  >
                    导出
                  </Button>
                  <Popconfirm
                    title="确认清空画布？"
                    onConfirm={clearCanvas}
                    okText="确认"
                    cancelText="取消"
                  >
                    <Button size="small" icon={<ClearOutlined />} danger>
                      清空
                    </Button>
                  </Popconfirm>
                </Space>
              </Panel>
            </ReactFlow>
          </div>
        </Dropdown>

        {/* 右侧 Pipeline 面板 */}
        <WorkflowPanel />
      </div>

      {/* 节点配置面板（Drawer 覆盖层，不占布局位） */}
      <NodeConfigPanel />

      {/* Agent-k 全局配置抽屉 */}
      <AgentKConfigDrawer open={agentKDrawerOpen} onClose={() => setAgentKDrawerOpen(false)} />
    </div>
  );
}

/** 外层必须包裹 ReactFlowProvider */
function WorkflowEditorPage() {
  return (
    <ReactFlowProvider>
      <WorkflowEditorInner />
    </ReactFlowProvider>
  );
}

export default WorkflowEditorPage;
