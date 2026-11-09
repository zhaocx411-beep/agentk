/**
 * /agents 旧地址兼容
 * 官方 / 我的原本是页内 Tab（?tab=mine），拆成二级菜单后各自独立成路由。
 * 这里把历史链接与收藏夹地址转发到新路由，并透传官方目录的 view / type 查询参数。
 */
import { Navigate, useSearchParams } from 'react-router-dom';

function AgentsIndexRedirect() {
  const [searchParams] = useSearchParams();
  const scope = searchParams.get('tab') === 'mine' ? 'mine' : 'official';

  const rest = new URLSearchParams(searchParams);
  rest.delete('tab');
  const query = rest.toString();

  return <Navigate to={`/agents/${scope}${query ? `?${query}` : ''}`} replace />;
}

export default AgentsIndexRedirect;
