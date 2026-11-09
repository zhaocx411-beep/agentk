/**
 * 官方目录「立即使用」：只负责跳转体验页，不创建会话
 * - 自主规划 / 工作流均进 /chat/new?...
 * - 工作流在页内 CreationStudio 配置；首次生成时再调 quick session
 */
import { useCallback, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import type { OfficialCatalogCardData } from '@/types/officialCatalog';
import { buildOfficialLaunchChatPath } from '../utils';

interface UseOfficialLaunchResult {
  launch: (item: OfficialCatalogCardData) => void;
  launching: boolean;
  modal: ReactNode;
}

export function useOfficialLaunch(): UseOfficialLaunchResult {
  const navigate = useNavigate();

  const launch = useCallback(
    (item: OfficialCatalogCardData) => {
      navigate(buildOfficialLaunchChatPath(item));
    },
    [navigate],
  );

  return { launch, launching: false, modal: null };
}
