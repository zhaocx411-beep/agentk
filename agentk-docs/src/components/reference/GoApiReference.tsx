import Link from '@docusaurus/Link';
import {useMemo, type ReactNode} from 'react';
import {DocRightSidebar} from '../layout/DocRightSidebar';
import styles from './GoApiReference.module.css';

type SymbolTarget = {
  packagePath: string;
  symbol: string;
  builtin: boolean;
};

type TextReference = {
  start: number;
  end: number;
  target: SymbolTarget;
};

type AnnotatedText = {
  text: string;
  references: TextReference[];
};

type ParameterDoc = {
  names: string[];
  type: AnnotatedText;
  variadic: boolean;
};

type TypeParameter = {
  name: string;
  constraint: AnnotatedText;
};

type FuncSignature = {
  receiver: ParameterDoc | null;
  typeParameters: TypeParameter[];
  parameters: ParameterDoc[];
  results: ParameterDoc[];
};

type ValueDoc = {
  names: string[];
  doc: string;
  declaration: AnnotatedText;
};

type FuncDoc = {
  name: string;
  doc: string;
  declaration: AnnotatedText;
  signature: FuncSignature;
};

type TypeDoc = {
  name: string;
  doc: string;
  declaration: AnnotatedText;
  consts: ValueDoc[];
  vars: ValueDoc[];
  funcs: FuncDoc[];
  methods: FuncDoc[];
};

type PackageDoc = {
  importPath: string;
  name: string;
  doc: string;
  consts: ValueDoc[];
  vars: ValueDoc[];
  funcs: FuncDoc[];
  types: TypeDoc[];
};

export type View = 'client' | 'worker' | 'protocol';

export type ApiViewModel = {
  modulePath: string;
  view: View;
  packages: PackageDoc[];
};

type IndexEntry = {
  name: string;
  href: string;
  children?: IndexEntry[];
};

const views: Record<View, {title: string; description: string}> = {
  client: {
    title: 'Go Client API',
    description: '创建和管理 Session、启动 Execution，并通过 KESS gRPC 连接 AgentK。',
  },
  worker: {
    title: 'Go Worker API',
    description: '发布 Capability、注册 Temporal 实现并运行 AgentK Worker。',
  },
  protocol: {
    title: 'Go Protocol Types',
    description: 'Go SDK 暴露的 protobuf message、enum 与 oneof 类型。',
  },
};

const viewLinks: Array<{view: View; label: string; to: string}> = [
  {view: 'client', label: 'Client', to: '/reference/go/client'},
  {view: 'worker', label: 'Worker', to: '/reference/go/worker'},
  {view: 'protocol', label: 'Protocol', to: '/reference/go/protocol'},
];

function symbolId(path: string): string {
  return path.replace(/[^a-zA-Z0-9_-]/g, '-');
}

function packageLabel(modulePath: string, importPath: string): string {
  return importPath.slice(modulePath.length + 1);
}

function packageView(modulePath: string, importPath: string): View | undefined {
  if (!importPath.startsWith(`${modulePath}/`)) return undefined;
  const relative = packageLabel(modulePath, importPath);
  if (relative === 'client' || relative === 'client/grpc') return 'client';
  if (relative === 'temporal' || relative === 'worker' || relative.startsWith('worker/')) return 'worker';
  if (['gen/api/v1', 'gen/capability/v1', 'gen/common/v1', 'gen/invocation/v1'].includes(relative)) return 'protocol';
  return undefined;
}

function referenceHref(modulePath: string, target: SymbolTarget): string | undefined {
  if (target.builtin) return `https://pkg.go.dev/builtin#${target.symbol}`;
  const localView = packageView(modulePath, target.packagePath);
  if (localView) {
    const targetPath = target.symbol
      ? `${target.packagePath}.${target.symbol}`
      : target.packagePath;
    return `/reference/go/${localView}#${symbolId(targetPath)}`;
  }
  if (!target.packagePath || target.packagePath.startsWith(`${modulePath}/`)) return undefined;
  const fragment = target.symbol ? `#${target.symbol}` : '';
  return `https://pkg.go.dev/${target.packagePath}${fragment}`;
}

function valueId(importPath: string, value: ValueDoc): string {
  return symbolId(`${importPath}.${value.names[0]}`);
}

function functionId(importPath: string, fn: FuncDoc): string {
  return symbolId(`${importPath}.${fn.name}`);
}

function sectionId(importPath: string, section: string): string {
  return symbolId(`${importPath}.${section}`);
}

function functionSynopsis(fn: FuncDoc): string {
  const parameters = fn.signature.parameters.flatMap((parameter) => {
    if (parameter.names.length > 0) return parameter.names;
    const prefix = parameter.variadic ? '...' : '';
    return [`${prefix}${parameter.type.text}`];
  });
  return `${fn.name}(${parameters.join(', ')})`;
}

function DocText({value}: {value: string}): ReactNode {
  if (!value.trim()) return null;
  return (
    <div className={styles.docText}>
      {value.trim().split(/\n{2,}/).map((paragraph, index) => (
        <p key={index}>{paragraph}</p>
      ))}
    </div>
  );
}

type TextRange = {
  start: number;
  end: number;
};

function rawStringRanges(text: string): TextRange[] {
  const characters = Array.from(text);
  const ranges: TextRange[] = [];

  for (let index = 0; index < characters.length;) {
    if (characters[index] === '/' && characters[index + 1] === '/') {
      index += 2;
      while (index < characters.length && characters[index] !== '\n') index += 1;
      continue;
    }
    if (characters[index] === '/' && characters[index + 1] === '*') {
      index += 2;
      while (
        index < characters.length &&
        !(characters[index] === '*' && characters[index + 1] === '/')
      ) index += 1;
      index = Math.min(index + 2, characters.length);
      continue;
    }
    if (characters[index] === '"' || characters[index] === "'") {
      const quote = characters[index];
      index += 1;
      while (index < characters.length) {
        if (characters[index] === '\\') {
          index += 2;
        } else if (characters[index] === quote) {
          index += 1;
          break;
        } else {
          index += 1;
        }
      }
      continue;
    }
    if (characters[index] === '`') {
      const start = index;
      index += 1;
      while (index < characters.length && characters[index] !== '`') index += 1;
      if (index < characters.length) {
        index += 1;
        ranges.push({start, end: index});
      }
      continue;
    }
    index += 1;
  }

  return ranges;
}

function AnnotatedCode({
  modulePath,
  softenStructTags = false,
  value,
}: {
  modulePath: string;
  softenStructTags?: boolean;
  value: AnnotatedText;
}): ReactNode {
  const characters = Array.from(value.text);
  const rendered: ReactNode[] = [];
  const structTags = softenStructTags ? rawStringRanges(value.text) : [];
  let offset = 0;
  let structTagIndex = 0;

  const appendPlainText = (start: number, end: number): void => {
    while (structTags[structTagIndex]?.end <= start) structTagIndex += 1;
    let cursor = start;
    while (structTags[structTagIndex]?.start < end) {
      const tag = structTags[structTagIndex];
      if (tag.start < cursor || tag.end > end) break;
      rendered.push(characters.slice(cursor, tag.start).join(''));
      rendered.push(
        <span className={styles.structTag} key={`tag:${tag.start}:${tag.end}`}>
          {characters.slice(tag.start, tag.end).join('')}
        </span>,
      );
      cursor = tag.end;
      structTagIndex += 1;
    }
    rendered.push(characters.slice(cursor, end).join(''));
  };

  value.references.forEach((reference, index) => {
    if (
      reference.start < offset ||
      reference.end < reference.start ||
      reference.end > characters.length
    ) {
      return;
    }
    appendPlainText(offset, reference.start);
    const label = characters.slice(reference.start, reference.end).join('');
    const href = referenceHref(modulePath, reference.target);
    rendered.push(
      href
        ? <a href={href} key={`${reference.start}:${reference.end}:${index}`}>{label}</a>
        : label,
    );
    offset = reference.end;
  });
  appendPlainText(offset, characters.length);
  return <>{rendered}</>;
}

function Declaration({
  modulePath,
  softenStructTags,
  value,
}: {
  modulePath: string;
  softenStructTags?: boolean;
  value: AnnotatedText;
}): ReactNode {
  return (
    <div className={styles.declaration}>
      <pre><code><AnnotatedCode modulePath={modulePath} softenStructTags={softenStructTags} value={value} /></code></pre>
    </div>
  );
}

function ValueReference({
  importPath,
  kind,
  modulePath,
  value,
}: {
  importPath: string;
  kind: 'const' | 'var';
  modulePath: string;
  value: ValueDoc;
}): ReactNode {
  return (
    <article className={styles.symbol} id={valueId(importPath, value)}>
      <h4><span>{kind}</span> <code>{value.names.join(', ')}</code></h4>
      <Declaration modulePath={modulePath} value={value.declaration} />
      <DocText value={value.doc} />
    </article>
  );
}

function FuncReference({
  fn,
  importPath,
  kind,
  modulePath,
}: {
  fn: FuncDoc;
  importPath: string;
  kind: 'func' | 'method';
  modulePath: string;
}): ReactNode {
  return (
    <article className={styles.symbol} id={functionId(importPath, fn)}>
      <h4><span>{kind}</span> <code>{fn.name}</code></h4>
      <Declaration modulePath={modulePath} value={fn.declaration} />
      <DocText value={fn.doc} />
    </article>
  );
}

function ValueGroup({
  importPath,
  kind,
  modulePath,
  section,
  title,
  values,
}: {
  importPath: string;
  kind: 'const' | 'var';
  modulePath: string;
  section?: string;
  title: string;
  values: ValueDoc[];
}): ReactNode {
  if (values.length === 0) return null;
  return (
    <section className={styles.group} id={section ? sectionId(importPath, section) : undefined}>
      <h3>{title}</h3>
      {values.map((value) => (
        <ValueReference
          importPath={importPath}
          key={value.names.join('\0')}
          kind={kind}
          modulePath={modulePath}
          value={value}
        />
      ))}
    </section>
  );
}

function FuncGroup({
  funcs,
  importPath,
  kind,
  modulePath,
  section,
  title,
}: {
  funcs: FuncDoc[];
  importPath: string;
  kind: 'func' | 'method';
  modulePath: string;
  section?: string;
  title: string;
}): ReactNode {
  if (funcs.length === 0) return null;
  return (
    <section className={styles.group} id={section ? sectionId(importPath, section) : undefined}>
      <h3>{title}</h3>
      {funcs.map((fn) => (
        <FuncReference fn={fn} importPath={importPath} key={fn.name} kind={kind} modulePath={modulePath} />
      ))}
    </section>
  );
}

function TypeReference({
  importPath,
  modulePath,
  type,
}: {
  importPath: string;
  modulePath: string;
  type: TypeDoc;
}): ReactNode {
  const typePath = `${importPath}.${type.name}`;
  return (
    <article className={styles.type} id={symbolId(typePath)}>
      <header className={styles.typeHeading}>
        <p>type</p>
        <h3><code>{type.name}</code></h3>
      </header>
      <Declaration modulePath={modulePath} softenStructTags value={type.declaration} />
      <DocText value={type.doc} />
      <ValueGroup importPath={typePath} kind="const" modulePath={modulePath} title="Constants" values={type.consts} />
      <ValueGroup importPath={typePath} kind="var" modulePath={modulePath} title="Variables" values={type.vars} />
      <FuncGroup funcs={type.funcs} importPath={typePath} kind="func" modulePath={modulePath} title="Functions" />
      <FuncGroup funcs={type.methods} importPath={typePath} kind="method" modulePath={modulePath} title="Methods" />
    </article>
  );
}

function typeIndexEntry(pkg: PackageDoc, type: TypeDoc): IndexEntry {
  const path = `${pkg.importPath}.${type.name}`;
  return {
    name: type.name,
    href: `#${symbolId(path)}`,
    children: [...type.funcs, ...type.methods].map((fn) => ({
      name: functionSynopsis(fn),
      href: `#${functionId(path, fn)}`,
    })),
  };
}

function PackageIndex({pkg}: {pkg: PackageDoc}): ReactNode {
  const groups: Array<{title: string; entries: IndexEntry[]}> = [
    {
      title: 'Constants',
      entries: pkg.consts.flatMap((value) =>
        value.names.map((name) => ({name, href: `#${valueId(pkg.importPath, value)}`})),
      ),
    },
    {
      title: 'Variables',
      entries: pkg.vars.flatMap((value) =>
        value.names.map((name) => ({name, href: `#${valueId(pkg.importPath, value)}`})),
      ),
    },
    {
      title: 'Functions',
      entries: pkg.funcs.map((fn) => ({
        name: functionSynopsis(fn),
        href: `#${functionId(pkg.importPath, fn)}`,
      })),
    },
    {title: 'Types', entries: pkg.types.map((type) => typeIndexEntry(pkg, type))},
  ].filter((group) => group.entries.length > 0);

  if (groups.length === 0) return null;
  return (
    <nav
      aria-label={`${pkg.name} package index`}
      className={styles.packageIndex}
      id={sectionId(pkg.importPath, 'index')}
    >
      <p>Index</p>
      <div>
        {groups.map((group) => (
          <section key={group.title}>
            <h3>{group.title}</h3>
            <ul>
              {group.entries.map((entry) => (
                <li key={entry.name}>
                  <a href={entry.href}>{entry.name}</a>
                  {entry.children && entry.children.length > 0 && (
                    <ul>
                      {entry.children.map((child) => (
                        <li key={child.name}><a href={child.href}>{child.name}</a></li>
                      ))}
                    </ul>
                  )}
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </nav>
  );
}

function PackageReference({modulePath, pkg}: {modulePath: string; pkg: PackageDoc}): ReactNode {
  return (
    <section className={styles.package} id={symbolId(pkg.importPath)}>
      <header className={styles.packageHeading}>
        <p>package</p>
        <h2><code>{pkg.name}</code></h2>
        <code>{pkg.importPath}</code>
      </header>
      <DocText value={pkg.doc} />
      <PackageIndex pkg={pkg} />
      <ValueGroup importPath={pkg.importPath} kind="const" modulePath={modulePath} section="constants" title="Constants" values={pkg.consts} />
      <ValueGroup importPath={pkg.importPath} kind="var" modulePath={modulePath} section="variables" title="Variables" values={pkg.vars} />
      <FuncGroup funcs={pkg.funcs} importPath={pkg.importPath} kind="func" modulePath={modulePath} section="functions" title="Functions" />
      {pkg.types.length > 0 && (
        <section className={styles.types} id={sectionId(pkg.importPath, 'types')}>
          <h3>Types</h3>
          {pkg.types.map((type) => (
            <TypeReference importPath={pkg.importPath} key={type.name} modulePath={modulePath} type={type} />
          ))}
        </section>
      )}
      <a className={styles.backToTop} href="#go-reference-top">Back to top ↑</a>
    </section>
  );
}

function valueEntries(importPath: string, values: ValueDoc[]): IndexEntry[] {
  return values.flatMap((value) =>
    value.names.map((name) => ({name, href: `#${valueId(importPath, value)}`})),
  );
}

function packageTocGroups(pkg: PackageDoc): Array<{name: string; href: string; entries: IndexEntry[]}> {
  return [
    {name: 'Overview', href: `#${symbolId(pkg.importPath)}`, entries: []},
    {name: 'Index', href: `#${sectionId(pkg.importPath, 'index')}`, entries: []},
    {
      name: 'Constants',
      href: `#${sectionId(pkg.importPath, 'constants')}`,
      entries: valueEntries(pkg.importPath, pkg.consts),
    },
    {
      name: 'Variables',
      href: `#${sectionId(pkg.importPath, 'variables')}`,
      entries: valueEntries(pkg.importPath, pkg.vars),
    },
    {
      name: 'Functions',
      href: `#${sectionId(pkg.importPath, 'functions')}`,
      entries: pkg.funcs.map((fn) => ({
        name: functionSynopsis(fn),
        href: `#${functionId(pkg.importPath, fn)}`,
      })),
    },
    {
      name: 'Types',
      href: `#${sectionId(pkg.importPath, 'types')}`,
      entries: pkg.types.map((type) => typeIndexEntry(pkg, type)),
    },
  ].filter((group) => ['Overview', 'Index'].includes(group.name) || group.entries.length > 0);
}

function TocEntries({entries}: {entries: IndexEntry[]}): ReactNode {
  if (entries.length === 0) return null;
  return (
    <ul>
      {entries.map((entry) => (
        <li key={entry.name}>
          <a href={entry.href}>{entry.name}</a>
          {entry.children && <TocEntries entries={entry.children} />}
        </li>
      ))}
    </ul>
  );
}

function ReferenceToc({modulePath, packages}: {modulePath: string; packages: PackageDoc[]}): ReactNode {
  return (
    <aside className={styles.toc}>
      <nav aria-label="On this page">
        <p>Documentation</p>
        {packages.map((pkg) => (
          <div className={styles.tocPackage} key={pkg.importPath}>
            <a className={styles.tocPackageName} href={`#${symbolId(pkg.importPath)}`}>
              {packageLabel(modulePath, pkg.importPath)}
            </a>
            <ul className={styles.tocGroups}>
              {packageTocGroups(pkg).map((group) => (
                <li key={group.name}>
                  <a href={group.href}>{group.name}</a>
                  <TocEntries entries={group.entries} />
                </li>
              ))}
            </ul>
          </div>
        ))}
      </nav>
    </aside>
  );
}

function symbolCount(packages: PackageDoc[]): number {
  return packages.reduce(
    (total, pkg) =>
      total +
      pkg.consts.reduce((count, value) => count + value.names.length, 0) +
      pkg.vars.reduce((count, value) => count + value.names.length, 0) +
      pkg.funcs.length +
      pkg.types.reduce(
        (count, type) =>
          count +
          1 +
          type.consts.reduce((sum, value) => sum + value.names.length, 0) +
          type.vars.reduce((sum, value) => sum + value.names.length, 0) +
          type.funcs.length +
          type.methods.length,
        0,
      ),
    0,
  );
}

export default function GoApiReference({data, view}: {data: ApiViewModel; view: View}): ReactNode {
  if (data.view !== view) throw new Error(`Expected Go ${view} API data, received ${data.view}`);
  const config = views[view];
  const toc = useMemo(
    () => <ReferenceToc modulePath={data.modulePath} packages={data.packages} />,
    [data.modulePath, data.packages],
  );

  return (
    <div className={styles.reference} id="go-reference-top">
      <DocRightSidebar>{toc}</DocRightSidebar>
      <header className={styles.pageHeader}>
        <p>Go API Reference</p>
        <h1>{config.title}</h1>
        <div className={styles.summary}>
          <span>{data.packages.length} packages</span>
          <span>{symbolCount(data.packages)} symbols</span>
        </div>
        <p>{config.description}</p>
      </header>

      <nav className={styles.views} aria-label="Go API Reference sections">
        {viewLinks.map((item) => (
          <Link className={item.view === view ? styles.activeView : undefined} key={item.view} to={item.to}>
            {item.label}
          </Link>
        ))}
      </nav>

      <nav className={styles.packageNav} aria-label="Packages on this page">
        <span>Packages</span>
        {data.packages.map((pkg) => (
          <a href={`#${symbolId(pkg.importPath)}`} key={pkg.importPath}>
            {packageLabel(data.modulePath, pkg.importPath)}
          </a>
        ))}
      </nav>

      {data.packages.map((pkg) => (
        <PackageReference key={pkg.importPath} modulePath={data.modulePath} pkg={pkg} />
      ))}
    </div>
  );
}
