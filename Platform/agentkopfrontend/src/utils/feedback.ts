/**
 * antd 反馈方法（message / modal）的全局桥接
 *
 * antd 的静态 `message.xxx`、`Modal.confirm` 拿不到 ConfigProvider 的动态 theme，
 * 开发环境会打「Static function can not consume context」警告；官方给的替代是
 * `App.useApp()`，但它只能在组件里调用，request.ts 这类非组件模块用不上。
 *
 * 这里由根节点通过 bindFeedback 把 App 提供的实例交进来，业务侧统一从本模块 import，
 * 用法与 antd 静态方法完全一致。绑定之前（极早期的模块副作用）回落到静态方法。
 */
import { Modal as StaticModal, message as staticMessage } from 'antd';
import type { useAppProps } from 'antd/es/app/context';
import type { MessageInstance } from 'antd/es/message/interface';
import type { HookAPI as ModalApi } from 'antd/es/modal/useModal';

let messageApi: MessageInstance | null = null;
let modalApi: ModalApi | null = null;

const msg = () => messageApi ?? staticMessage;
const mdl = () => modalApi ?? (StaticModal as unknown as ModalApi);

export const message: MessageInstance = {
  info: (...args) => msg().info(...args),
  success: (...args) => msg().success(...args),
  error: (...args) => msg().error(...args),
  warning: (...args) => msg().warning(...args),
  loading: (...args) => msg().loading(...args),
  open: (args) => msg().open(args),
  destroy: (key) => msg().destroy(key),
};

export const modal: ModalApi = {
  info: (config) => mdl().info(config),
  success: (config) => mdl().success(config),
  error: (config) => mdl().error(config),
  warning: (config) => mdl().warning(config),
  confirm: (config) => mdl().confirm(config),
};

/** 由挂在 antd App 内部的组件调用，交出带 theme 的实例 */
export function bindFeedback(api: useAppProps) {
  messageApi = api.message;
  modalApi = api.modal;
}
