import type { ReactNode } from 'react';
import styles from './WorkbenchLayout.module.less';

type WorkbenchVariant = 'configuration' | 'balanced' | 'two-pane';
type WorkbenchTone = 'default' | 'muted';

interface WorkbenchLayoutProps {
  children: ReactNode;
  variant?: WorkbenchVariant;
  className?: string;
}

interface WorkbenchPaneProps {
  header: ReactNode;
  children: ReactNode;
  tone?: WorkbenchTone;
  className?: string;
  contentClassName?: string;
  ariaLabel?: string;
}

const variantClassMap: Record<WorkbenchVariant, string> = {
  configuration: styles.configuration,
  balanced: styles.balanced,
  'two-pane': styles.twoPane,
};

export function WorkbenchLayout({
  children,
  variant = 'balanced',
  className,
}: WorkbenchLayoutProps) {
  return (
    <div className={[styles.layout, variantClassMap[variant], className].filter(Boolean).join(' ')}>
      {children}
    </div>
  );
}

export function WorkbenchPane({
  header,
  children,
  tone = 'default',
  className,
  contentClassName,
  ariaLabel,
}: WorkbenchPaneProps) {
  return (
    <section
      className={[styles.pane, tone === 'muted' ? styles.paneMuted : styles.paneDefault, className]
        .filter(Boolean)
        .join(' ')}
      aria-label={ariaLabel}
    >
      <header className={styles.paneHeader}>{header}</header>
      <div className={[styles.paneContent, contentClassName].filter(Boolean).join(' ')}>
        {children}
      </div>
    </section>
  );
}
