/**
 * CustomAgentCreate - 自定义 Agent 一级菜单入口
 * 进入即为新建配置页，先弹出固定命名弹窗；填写后走与「创建 Agent」相同的创建流程
 */
import CreateAgentModal from '../components/CreateAgentModal';
import { useGoBack } from '@/hooks';
import { NewAgentWorkbench } from './NewAgentWorkbench';

function CustomAgentCreate() {
  const goBack = useGoBack('/agents/mine');

  return (
    <>
      <NewAgentWorkbench onBack={goBack} />
      <CreateAgentModal open lockDismiss onClose={goBack} />
    </>
  );
}

export default CustomAgentCreate;
