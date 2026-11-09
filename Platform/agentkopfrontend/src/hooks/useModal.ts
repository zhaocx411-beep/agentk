import { useState, useCallback } from 'react';

interface UseModalResult {
  /** 弹窗是否可见 */
  visible: boolean;
  /** 打开弹窗 */
  open: (data?: unknown) => void;
  /** 关闭弹窗 */
  close: () => void;
  /** 传入弹窗的数据（如编辑回填数据） */
  modalData: unknown;
  /** 确认 loading 状态 */
  confirmLoading: boolean;
  /** 设置确认 loading */
  setConfirmLoading: (loading: boolean) => void;
}

/**
 * useModal - 通用弹窗控制 Hook
 * 管理弹窗的打开/关闭、数据传递、loading 状态
 *
 * @example
 * const { visible, open, close, modalData } = useModal();
 * <Modal open={visible} onCancel={close}>...</Modal>
 */
export function useModal(): UseModalResult {
  const [visible, setVisible] = useState(false);
  const [modalData, setModalData] = useState<unknown>(null);
  const [confirmLoading, setConfirmLoading] = useState(false);

  const open = useCallback((data?: unknown) => {
    setModalData(data ?? null);
    setVisible(true);
  }, []);

  const close = useCallback(() => {
    setVisible(false);
    setModalData(null);
    setConfirmLoading(false);
  }, []);

  return {
    visible,
    open,
    close,
    modalData,
    confirmLoading,
    setConfirmLoading,
  };
}
