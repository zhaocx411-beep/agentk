/**
 * 顶部导航：Logo + 水平菜单 + 用户信息
 * 移动端收起菜单为抽屉触发按钮
 */
import { useMemo } from 'react';
import { Layout, Button, Avatar, Space, Menu } from 'antd';
import { MenuOutlined, UserOutlined } from '@ant-design/icons';
import { useNavigate, useLocation } from 'react-router-dom';
import { isSuperAdminUser, useUserStore } from '@/store/user';
import { colors } from '@/styles/design-tokens';
import { LOCAL_ROUTE_MENU_ENABLED } from '@/config/localFeatures';
import { buildPermittedMenuItems, resolveGroupKey, resolveSelectedKey } from '../../menuConfig';
import styles from './Header.module.less';

const { Header: AntHeader } = Layout;

interface HeaderProps {
  /** 移动端：打开抽屉菜单 */
  onOpenMobileMenu?: () => void;
  /** 是否展示汉堡按钮（移动端） */
  showMobileTrigger?: boolean;
}

function Header({ onOpenMobileMenu, showMobileTrigger = false }: HeaderProps) {
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
  // 顶栏高亮当前分组；下拉展开时仍按路由叶子选中
  const selectedKeys = [selectedKey, ...(activeGroupKey ? [activeGroupKey] : [])];

  return (
    <AntHeader className={styles.header}>
      <div className={styles.left}>
        {showMobileTrigger ? (
          <Button
            type="text"
            icon={<MenuOutlined />}
            onClick={onOpenMobileMenu}
            className={styles.trigger}
            aria-label="打开导航菜单"
          />
        ) : null}

        <button
          type="button"
          className={styles.brand}
          onClick={() => navigate('/')}
          aria-label="回到可灵Agent中后台首页"
        >
          <span className={styles.logoMark}>K</span>
          <span className={styles.logoText}>可灵Agent中后台</span>
        </button>

        {!showMobileTrigger ? (
          <Menu
            mode="horizontal"
            selectedKeys={selectedKeys}
            items={menuItems}
            onClick={({ key }) => {
              // 一级分组不可跳转，只响应二级路由 key
              if (key.startsWith('/')) navigate(key);
            }}
            className={styles.menu}
          />
        ) : null}
      </div>

      <div className={styles.right}>
        <Space className={styles.userInfo} size={8}>
          <Avatar
            size="small"
            icon={<UserOutlined />}
            style={{ backgroundColor: colors.primary }}
          />
          <span className={styles.username}>{userInfo?.name || userInfo?.username || '用户'}</span>
        </Space>
      </div>
    </AntHeader>
  );
}

export default Header;
