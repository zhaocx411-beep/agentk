/**
 * useGoBack - 返回上一页，并在没有应用内历史时回落到指定路径
 *
 * 详情页的返回按钮如果写死目标路径，会丢掉列表页 URL 上的分页、筛选等状态。
 * 走浏览器历史回退可以原样恢复来源 URL；只有在直接打开详情页（无可回退历史）时才用兜底路径。
 */
import { useCallback } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';

export function useGoBack(fallback: string) {
  const navigate = useNavigate();
  const location = useLocation();

  return useCallback(() => {
    // react-router 对「首次进入」的 location 使用固定 key 'default'，
    // 即用户是直接输入地址或刷新进来的，此时回退会离开本应用
    if (location.key === 'default') {
      navigate(fallback);
      return;
    }
    navigate(-1);
  }, [navigate, location.key, fallback]);
}
