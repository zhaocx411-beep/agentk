import { Suspense, useEffect, type ReactNode } from 'react';
import { Spin } from 'antd';
import { Navigate, useLocation } from 'react-router-dom';
import { useUserStore } from '@/store/user';

interface AuthGuardProps {
  children: ReactNode;
}

/**
 * 路由守卫组件
 * - 加载用户信息（ensureUserInfoLoaded）
 * - Suspense 懒加载兜底
 * - 菜单权限校验（hasMenuPermission）
 */
function AuthGuard({ children }: AuthGuardProps) {
  const location = useLocation();
  const { loaded, loading, ensureUserInfoLoaded, hasRoutePermission } = useUserStore();

  useEffect(() => {
    ensureUserInfoLoaded();
  }, [ensureUserInfoLoaded]);

  // 用户信息加载中
  if (!loaded || loading) {
    return (
      <div
        style={{
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          height: '100vh',
        }}
      >
        <Spin size="large" />
      </div>
    );
  }

  // 权限校验：无权限则跳转首页
  if (!hasRoutePermission(location.pathname)) {
    return <Navigate to="/" replace />;
  }

  return (
    <Suspense
      fallback={
        <div
          style={{
            display: 'flex',
            justifyContent: 'center',
            alignItems: 'center',
            height: '100vh',
          }}
        >
          <Spin size="large" />
        </div>
      }
    >
      {children}
    </Suspense>
  );
}

export default AuthGuard;
