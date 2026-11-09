import {useCallback, useState, type ReactNode} from 'react';
import clsx from 'clsx';
import {useDoc} from '@docusaurus/plugin-content-docs/client';
import {useWindowSize} from '@docusaurus/theme-common';
import ContentVisibility from '@theme/ContentVisibility';
import DocBreadcrumbs from '@theme/DocBreadcrumbs';
import DocItemContent from '@theme/DocItem/Content';
import DocItemFooter from '@theme/DocItem/Footer';
import DocItemPaginator from '@theme/DocItem/Paginator';
import DocItemTOCDesktop from '@theme/DocItem/TOC/Desktop';
import DocItemTOCMobile from '@theme/DocItem/TOC/Mobile';
import DocVersionBadge from '@theme/DocVersionBadge';
import DocVersionBanner from '@theme/DocVersionBanner';
import type {Props} from '@theme/DocItem/Layout';
import {DocRightSidebarProvider} from '@site/src/components/layout/DocRightSidebar';

import styles from './styles.module.css';

function useDocTOC() {
  const {frontMatter, toc} = useDoc();
  const windowSize = useWindowSize();
  const hidden = frontMatter.hide_table_of_contents;
  const canRender = !hidden && toc.length > 0;

  return {
    hidden,
    mobile: canRender ? <DocItemTOCMobile /> : undefined,
    desktop:
      canRender && (windowSize === 'desktop' || windowSize === 'ssr')
        ? <DocItemTOCDesktop />
        : undefined,
  };
}

export default function DocItemLayout({children}: Props): ReactNode {
  const docTOC = useDocTOC();
  const {frontMatter, metadata} = useDoc();
  const customTOC = Boolean(
    (frontMatter as {custom_table_of_contents?: boolean}).custom_table_of_contents,
  );
  const [rightSidebar, setRightSidebar] = useState<ReactNode>(null);
  const registerRightSidebar = useCallback((content: ReactNode | null) => {
    setRightSidebar(() => content);
  }, []);
  const desktopSidebar = customTOC ? rightSidebar : docTOC.desktop;

  return (
    <DocRightSidebarProvider setSidebar={registerRightSidebar}>
      <div className="row">
        <div className={clsx('col', (!docTOC.hidden || customTOC) && styles.docItemCol)}>
          <ContentVisibility metadata={metadata} />
          <DocVersionBanner />
          <div className={styles.docItemContainer}>
            <article>
              <DocBreadcrumbs />
              <DocVersionBadge />
              {docTOC.mobile}
              <DocItemContent>{children}</DocItemContent>
              <DocItemFooter />
            </article>
            <DocItemPaginator />
          </div>
        </div>
        {(customTOC || desktopSidebar) && (
          <div className={clsx('col col--3', styles.rightSidebarCol)}>
            {desktopSidebar}
          </div>
        )}
      </div>
    </DocRightSidebarProvider>
  );
}
