import OriginalMDXComponents from '@theme-original/MDXComponents';
import type {ComponentProps} from 'react';
import {
  Artifact,
  Capability,
  EventLog,
  Execution,
  Pipeline,
  Profile,
  Session,
  Tool,
} from '@site/src/components/terms/Term';

const OriginalAnchor = OriginalMDXComponents.a;

const faviconByHost: Record<string, string> = {
  'techlink.corp.kuaishou.com':
    'https://cdnfile.corp.kuaishou.com/kc/files/a/infra-docs-site/docs/sites/prod/favicon.png',
};

function DocLink({href, className, children, ...props}: ComponentProps<'a'>) {
  if (!href?.match(/^https?:\/\//i)) {
    return (
      <OriginalAnchor href={href} className={className} {...props}>
        {children}
      </OriginalAnchor>
    );
  }

  const url = new URL(href);
  const favicon = faviconByHost[url.hostname] ?? `${url.origin}/favicon.ico`;

  return (
    <a
      href={href}
      className={['external-doc-link', className].filter(Boolean).join(' ')}
      target={props.target ?? '_blank'}
      rel={props.rel ?? 'noopener noreferrer'}
      {...props}
    >
      <img
        className="external-doc-link__favicon"
        src={favicon}
        alt=""
        aria-hidden="true"
        onError={(event) => {
          event.currentTarget.style.display = 'none';
        }}
      />
      <span>{children}</span>
    </a>
  );
}

export default {
  ...OriginalMDXComponents,
  a: DocLink,
  Artifact,
  Capability,
  EventLog,
  Execution,
  Pipeline,
  Profile,
  Session,
  Tool,
};
