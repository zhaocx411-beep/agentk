/**
 * 离线跑数页（二级菜单「批量跑数 / 离线跑数」）
 * 原「离线跑数」页内的任务 Tab，拆为独立路由 /offline-run/jobs
 */
import { RiFlaskLine } from 'react-icons/ri';
import PageContainer from '@/components/PageContainer';
import OfflineRunTab from './components/OfflineRunTab';
import styles from './OfflineRun.module.less';

function JobManage() {
  return (
    <PageContainer title="离线跑数">
      <div className={styles.subtitle}>
        <RiFlaskLine className={styles.subtitleIcon} />
        批量运行 Agent 任务并查看结果
      </div>

      <OfflineRunTab />
    </PageContainer>
  );
}

export default JobManage;
