/**
 * 后台主布局
 * 桌面端：顶部导航（Logo + 水平菜单 + 用户）+ 内容区
 * 移动端：顶栏汉堡 + 抽屉菜单 + 内容区
 */
import { useState } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Layout, Drawer } from 'antd';
import Sider from './components/Sider';
import Header from './components/Header';
import { useDocumentTitle, useResponsive } from '@/hooks';
import styles from './MainLayout.module.less';

const { Content } = Layout;

function MainLayout() {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { isMobile } = useResponsive();
  const { pathname } = useLocation();
  // 目录类页面自带 Hero 与内边距，内容区不再叠加 padding
  const flushContent =
    pathname === '/' ||
    pathname === '/agents/official' ||
    pathname.startsWith('/agents/official/app/') ||
    pathname.startsWith('/agents/official/chat/') ||
    pathname === '/agents/mine' ||
    pathname === '/tools' ||
    pathname.startsWith('/tools/') ||
    pathname === '/oss-tools' ||
    pathname === '/agent100' ||
    pathname.startsWith('/agent100/');

  useDocumentTitle();

  return (
    <Layout className={styles.layout}>
      {isMobile ? (
        <Drawer
          placement="left"
          open={drawerOpen}
          onClose={() => setDrawerOpen(false)}
          width={220}
          styles={{ body: { padding: 0 } }}
          closable={false}
        >
          <Sider onMenuClick={() => setDrawerOpen(false)} />
        </Drawer>
      ) : null}

      <Layout className={styles.contentWrapper}>
        <Header showMobileTrigger={isMobile} onOpenMobileMenu={() => setDrawerOpen(true)} />

        <Content className={`${styles.content} ${flushContent ? styles.contentFlush : ''}`}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}

export default MainLayout;
