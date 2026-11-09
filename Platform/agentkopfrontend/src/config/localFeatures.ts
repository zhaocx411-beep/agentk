/**
 * 本地开发开关：后端不可用时仍展示全部前端路由菜单并允许访问页面。
 * import.meta.env.DEV 保证生产构建即使误配变量也不会绕过权限。
 */
export const LOCAL_ROUTE_MENU_ENABLED =
  import.meta.env.DEV && import.meta.env.VITE_ENABLE_LOCAL_ROUTE_MENU === 'true';
