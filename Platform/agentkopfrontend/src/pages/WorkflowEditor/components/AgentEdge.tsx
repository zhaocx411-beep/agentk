import { memo, useCallback, useState, useRef, useEffect } from 'react';
import { BaseEdge, EdgeLabelRenderer, getBezierPath, type EdgeProps } from '@xyflow/react';
import { DeleteOutlined } from '@ant-design/icons';
import { useWorkflowStore } from '../store';
import type { AgentEdgeData } from '../types';

function AgentEdgeComponent({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  selected,
  style = {},
}: EdgeProps & { data?: AgentEdgeData }) {
  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  const deleteEdge = useWorkflowStore((s) => s.onEdgesChange);
  const [editText, setEditText] = useState(data?.label || '');
  const inputRef = useRef<HTMLTextAreaElement | null>(null);

  // 选中时自动聚焦输入框
  useEffect(() => {
    if (selected) {
      setEditText(data?.label || '');
      setTimeout(() => inputRef.current?.focus(), 50);
    }
  }, [selected, data?.label]);

  const handleLabelConfirm = useCallback(() => {
    const trimmed = editText.trim();
    useWorkflowStore.setState({
      edges: useWorkflowStore
        .getState()
        .edges.map((e) =>
          e.id === id ? { ...e, data: { ...e.data, label: trimmed || undefined } } : e,
        ),
    });
  }, [id, editText]);

  const handleDelete = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation();
      deleteEdge([{ id, type: 'remove' }]);
    },
    [id, deleteEdge],
  );

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        style={{
          ...style,
          stroke: selected ? '#1677ff' : '#b1b1b7',
          strokeWidth: selected ? 2 : 1.5,
          strokeDasharray: data?.animated ? '5 5' : undefined,
          animation: data?.animated ? 'dash 0.6s linear infinite' : undefined,
        }}
      />
      {/* 选中时在连线中点上方显示删除按钮 */}
      {selected && (
        <EdgeLabelRenderer>
          <div
            style={{
              position: 'absolute',
              transform: `translate(-50%, -135%) translate(${labelX}px, ${labelY - 10}px)`,
              pointerEvents: 'all',
            }}
            className="nodrag nopan"
          >
            <button
              onClick={handleDelete}
              style={{
                width: 20,
                height: 20,
                borderRadius: '50%',
                border: '1px solid #ff4d4f',
                background: '#fff',
                color: '#ff4d4f',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: 11,
                padding: 0,
                boxShadow: '0 1px 4px rgba(0,0,0,0.12)',
              }}
            >
              <DeleteOutlined />
            </button>
          </div>
        </EdgeLabelRenderer>
      )}
      {/* 标签区域：选中时为可编辑 Input，未选中时显示文本 */}
      <EdgeLabelRenderer>
        <div
          style={{
            position: 'absolute',
            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
            pointerEvents: 'all',
            fontSize: 11,
          }}
          className="nodrag nopan"
        >
          {selected ? (
            <textarea
              ref={inputRef as React.RefObject<HTMLTextAreaElement>}
              value={editText}
              maxLength={200}
              onChange={(e) => setEditText(e.target.value)}
              onBlur={handleLabelConfirm}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault();
                  handleLabelConfirm();
                }
              }}
              placeholder="输入标签"
              onWheel={(e) => e.stopPropagation()}
              style={{
                display: 'inline-block',
                maxWidth: 200,
                maxHeight: 48,
                overflow: 'auto',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
                fontSize: 11,
                border: '1px solid #1677ff',
                borderRadius: 4,
                padding: '2px 8px',
                outline: 'none',
                background: '#fff',
                color: '#1677ff',
                resize: 'none',
                lineHeight: '1.4',
              }}
            />
          ) : data?.label ? (
            <span
              onWheel={(e) => {
                e.stopPropagation();
              }}
              style={{
                display: 'inline-block',
                maxWidth: 200,
                maxHeight: 48,
                overflow: 'auto',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
                background: '#fff',
                padding: '2px 8px',
                borderRadius: 4,
                border: '1px solid #d9d9d9',
                color: 'rgba(0,0,0,0.65)',
                lineHeight: '1.4',
              }}
            >
              {data.label}
            </span>
          ) : null}
        </div>
      </EdgeLabelRenderer>
    </>
  );
}

export const AgentEdgeComponentMemo = memo(AgentEdgeComponent);
