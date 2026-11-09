/**
 * 移动端抽屉内的垂直导航（桌面已迁到顶栏）
 */
import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { Menu } from 'antd';
import type { MenuProps } from 'antd';
import { isSuperAdminUser, useUserStore } from '@/store/user';
import { LOCAL_ROUTE_MENU_ENABLED } from '@/config/localFeatures';
import { buildPermittedMenuItems, resolveGroupKey, resolveSelectedKey } from '../../menuConfig';
import styles from './Sider.module.less';

interface SiderProps {
  /** 菜单点击回调（关闭抽屉） */
  onMenuClick?: () => void;
}

function Sider({ onMenuClick }: SiderProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { userInfo, allowedMenuUrls } = useUserStore();
  const menuItems = useMemo(
    () =>
      buildPermittedMenuItems(
        allowedMenuUrls,
        isSuperAdminUser(userInfo),
        LOCAL_ROUTE_MENU_ENABLED,
      ),
    [allowedMenuUrls, userInfo],
  );

  const selectedKey = resolveSelectedKey(location.pathname);
  const activeGroupKey = resolveGroupKey(selectedKey);

  const [openKeys, setOpenKeys] = useState<string[]>(() =>
    activeGroupKey ? [activeGroupKey] : [],
  );

  useEffect(() => {
    if (!activeGroupKey) return;
    setOpenKeys((prev) => (prev.includes(activeGroupKey) ? prev : [...prev, activeGroupKey]));
  }, [activeGroupKey]);

  const handleMenuClick: MenuProps['onClick'] = ({ key }) => {
    if (!key.startsWith('/')) return;
    navigate(key);
    onMenuClick?.();
  };

  return (
    <div className={styles.drawerSider}>
      <div className={styles.logo}>
        <div className={styles.logoMark}>K</div>
        <span className={styles.logoText}>可灵Agent中后台</span>
      </div>

      <Menu
        mode="inline"
        selectedKeys={[selectedKey]}
        openKeys={openKeys}
        onOpenChange={(keys) => setOpenKeys(keys)}
        items={menuItems}
        onClick={handleMenuClick}
        className={styles.menu}
      />
    </div>
  );
}

export default Sider;
