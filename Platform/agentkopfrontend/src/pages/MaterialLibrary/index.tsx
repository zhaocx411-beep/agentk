import { Alert, Button, Empty, Pagination, Skeleton, Tag } from 'antd';
import { RiImageLine } from 'react-icons/ri';
import { MaterialCard } from './components/MaterialCard';
import { MaterialDetailModal } from './components/MaterialDetailModal';
import { MaterialToolbar } from './components/MaterialToolbar';
import { useMaterialLibraryList } from './useMaterialLibraryList';
import styles from './MaterialLibrary.module.less';

/** 素材库：按 category 浏览图片、音频与视频；短剧可再按剧名筛 */
function MaterialLibrary() {
  const list = useMaterialLibraryList();

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <div className={styles.titleBlock}>
          <h1 className={styles.title}>素材库</h1>
          <div className={styles.subtitle}>
            <RiImageLine className={styles.subtitleIcon} />
            按分类浏览图片、音频与视频
          </div>
        </div>
        <Tag className={styles.totalTag}>{list.total} 条素材</Tag>
      </div>

      <MaterialToolbar
        category={list.category}
        drama={list.drama}
        dramas={list.dramas}
        unlabeledDrama={list.unlabeledDrama}
        dramaLoading={list.dramaLoading}
        searching={list.isFetching && !list.isLoading}
        onCategoryChange={list.changeCategory}
        onDramaChange={list.changeDrama}
        onRefresh={() => void list.refetch()}
      />

      {list.isError && (
        <Alert
          type="error"
          showIcon
          message="素材加载失败"
          action={<Button onClick={() => void list.refetch()}>重新加载</Button>}
          className={styles.errorAlert}
        />
      )}

      <div className={styles.gridWrapper}>
        <Pagination
          className={styles.pagination}
          current={list.page}
          pageSize={list.pageSize}
          total={list.total}
          onChange={list.changePage}
          showSizeChanger={false}
        />

        {list.isLoading ? (
          <div className={styles.grid}>
            {Array.from({ length: 10 }, (_, index) => (
              <Skeleton.Node active className={styles.cardSkeleton} key={index} />
            ))}
          </div>
        ) : list.files.length > 0 ? (
          <div className={styles.grid} aria-busy={list.isFetching}>
            {list.files.map((item, index) => (
              <MaterialCard
                key={item.id}
                item={item}
                onClick={() => list.setSelectedIndex(index)}
              />
            ))}
          </div>
        ) : (
          !list.isError && (
            <div className={styles.empty}>
              <Empty description="没有符合条件的素材" />
            </div>
          )
        )}

        {list.total > list.pageSize && (
          <Pagination
            className={styles.pagination}
            current={list.page}
            pageSize={list.pageSize}
            total={list.total}
            onChange={list.changePage}
            showSizeChanger={false}
          />
        )}
      </div>

      <MaterialDetailModal
        key={list.selectedFile?.id ?? 'closed'}
        file={list.selectedFile}
        hasPrev={list.selectedIndex !== null && list.selectedIndex > 0}
        hasNext={
          list.selectedIndex !== null && list.selectedIndex < list.files.length - 1
        }
        onPrev={() =>
          list.setSelectedIndex((index) =>
            index !== null && index > 0 ? index - 1 : index,
          )
        }
        onNext={() =>
          list.setSelectedIndex((index) =>
            index !== null && index < list.files.length - 1 ? index + 1 : index,
          )
        }
        onClose={() => list.setSelectedIndex(null)}
      />
    </div>
  );
}

export default MaterialLibrary;
