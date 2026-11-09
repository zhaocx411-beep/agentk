import Link from '@docusaurus/Link';
import CodeBlock from '@theme/CodeBlock';
import {useMemo, type ReactNode} from 'react';
import {DocRightSidebar} from '../layout/DocRightSidebar';
import styles from './PythonApiReference.module.css';

type Parameter = {
  name: string;
  kind: string;
  annotation?: Annotation;
  default?: string;
};

type Annotation = {
  text: string;
  references?: Array<{
    start: number;
    end: number;
    target: string;
  }>;
};

export type Member = {
  kind: 'attribute' | 'class' | 'function' | 'property' | 'type_alias';
  definedAt?: string;
  doc?: string;
  scope?: 'class' | 'instance';
  annotation?: Annotation;
  value?: string;
  decorators?: string[];
  bases?: Annotation[];
  parameters?: Parameter[];
  returns?: Annotation;
  async?: boolean;
  members?: Record<string, Member>;
};

type ApiModule = {
  doc?: string;
  members: Record<string, Member>;
};

export type View = 'client' | 'worker' | 'protocol';

export type ApiViewModel = {
  package: string;
  view: View;
  modules: Record<string, ApiModule>;
};

type NamedMember = [name: string, member: Member];
type SymbolIndex = Map<string, string>;

type DocBlock =
  | {kind: 'code'; language?: string; section?: string; value: string}
  | {kind: 'field'; description: string; name: string; section?: string}
  | {kind: 'paragraph'; section?: string; value: string}
  | {kind: 'section'; value: string};

const views: Record<View, {title: string; description: string}> = {
  client: {
    title: 'Python Client API',
    description: '创建和管理 Session、启动 Execution，并通过 KESS gRPC 连接 AgentK。',
  },
  worker: {
    title: 'Python Worker API',
    description: '声明 Capability、发布 Tool 与 Workflow，并运行 AgentK Worker。',
  },
  protocol: {
    title: 'Python Protocol Types',
    description: 'Python SDK 暴露的 protobuf message 与 enum 类型。',
  },
};

const viewLinks: Array<{view: View; label: string; to: string}> = [
  {view: 'client', label: 'Client', to: '/reference/python/client'},
  {view: 'worker', label: 'Worker', to: '/reference/python/worker'},
  {view: 'protocol', label: 'Protocol', to: '/reference/python/protocol'},
];

function symbolId(path: string): string {
  return path.replace(/[^a-zA-Z0-9_-]/g, '-');
}

function parameterId(path: string, name: string): string {
  return `${symbolId(path)}-${symbolId(name)}`;
}

const parameterSections = new Set(['Args', 'Arguments', 'Parameters']);

function TargetMarker({id, label}: {id: string; label: string}): ReactNode {
  return (
    <a aria-label={`Permalink to ${label}`} className={styles.targetMarker} href={`#${id}`}>¤</a>
  );
}

function buildSymbolIndex(data: ApiViewModel): SymbolIndex {
  const candidates = new Map<string, Set<string>>();
  const add = (name: string, href: string) => {
    const targets = candidates.get(name) ?? new Set<string>();
    targets.add(href);
    candidates.set(name, targets);
  };

  Object.entries(data.modules).forEach(([moduleName, module]) => {
    add(moduleName, `#${symbolId(moduleName)}`);
    Object.entries(module.members).forEach(([name, member]) => {
      const path = `${moduleName}.${name}`;
      const href = `#${symbolId(path)}`;
      add(name, href);
      add(path, href);
      if (member.definedAt) add(member.definedAt, href);

      Object.entries(member.members ?? {}).forEach(([childName, child]) => {
        if (child.kind !== 'function') return;
        const childPath = `${path}.${childName}`;
        add(childName, `#${symbolId(childPath)}`);
        add(childPath, `#${symbolId(childPath)}`);
      });
    });
  });

  return new Map(
    [...candidates.entries()]
      .filter(([, targets]) => targets.size === 1)
      .map(([name, targets]) => [name, [...targets][0]]),
  );
}

function LinkedType({annotation, symbols}: {annotation: Annotation; symbols: SymbolIndex}): ReactNode {
  const rendered: ReactNode[] = [];
  let offset = 0;
  for (const reference of annotation.references ?? []) {
    if (reference.start < offset || reference.end > annotation.text.length) continue;
    rendered.push(annotation.text.slice(offset, reference.start));
    const label = annotation.text.slice(reference.start, reference.end);
    const href = symbols.get(reference.target);
    rendered.push(href ? <a href={href} key={`${reference.start}:${reference.target}`}>{label}</a> : label);
    offset = reference.end;
  }
  rendered.push(annotation.text.slice(offset));
  return rendered;
}

function parameterText(parameter: Parameter): string {
  const prefix =
    parameter.kind === 'variadic_positional'
      ? '*'
      : parameter.kind === 'variadic_keyword'
        ? '**'
        : '';
  const annotation = parameter.annotation ? `: ${parameter.annotation.text}` : '';
  const defaultValue = parameter.default === undefined ? '' : ` = ${parameter.default}`;
  return `${prefix}${parameter.name}${annotation}${defaultValue}`;
}

type SignaturePart =
  | {kind: 'marker'; value: '/' | '*'}
  | {kind: 'parameter'; parameter: Parameter};

function signatureParts(parameters: Parameter[] = []): SignaturePart[] {
  const rendered: SignaturePart[] = [];
  let hasVariadicPositional = false;
  let insertedKeywordMarker = false;

  parameters.forEach((parameter, index) => {
    if (
      parameter.kind === 'keyword_only' &&
      !hasVariadicPositional &&
      !insertedKeywordMarker
    ) {
      rendered.push({kind: 'marker', value: '*'});
      insertedKeywordMarker = true;
    }
    rendered.push({kind: 'parameter', parameter});
    if (parameter.kind === 'variadic_positional') hasVariadicPositional = true;
    if (
      parameter.kind === 'positional_only' &&
      parameters[index + 1]?.kind !== 'positional_only'
    ) {
      rendered.push({kind: 'marker', value: '/'});
    }
  });
  return rendered;
}

function ParameterSignature({href, parameter, symbols}: {href?: string; parameter: Parameter; symbols: SymbolIndex}): ReactNode {
  const prefix =
    parameter.kind === 'variadic_positional'
      ? '*'
      : parameter.kind === 'variadic_keyword'
        ? '**'
        : '';
  return (
    <>
      {prefix}{href ? <a href={href}>{parameter.name}</a> : parameter.name}
      {parameter.annotation && <>: <LinkedType annotation={parameter.annotation} symbols={symbols} /></>}
      {parameter.default !== undefined && <> = {parameter.default}</>}
    </>
  );
}

function CallableSignature({name, member, path, symbols}: {name: string; member: Member; path: string; symbols: SymbolIndex}): ReactNode {
  const parts = signatureParts(member.parameters);
  const documentedParameters = new Set(
    parseDocstring(member.doc ?? '')
      .filter((block): block is Extract<DocBlock, {kind: 'field'}> =>
        block.kind === 'field' && parameterSections.has(block.section ?? ''),
      )
      .map((block) => block.name),
  );
  const returnText = member.kind !== 'class' && member.returns ? ` -> ${member.returns.text}` : '';
  const length = name.length + 2 + returnText.length + parts.reduce((total, part) =>
    total + (part.kind === 'marker' ? 1 : parameterText(part.parameter).length) + 2,
  0);
  const multiline = length > 88 && parts.length > 0;

  return (
    <Signature>
      {name}({multiline && '\n'}
      {parts.map((part, index) => (
        <span key={index}>
          {multiline && '    '}
          {part.kind === 'marker'
            ? part.value
            : <ParameterSignature
                href={
                  documentedParameters.has(part.parameter.name)
                    ? `#${parameterId(path, part.parameter.name)}`
                    : member.kind === 'class' && ['attribute', 'property'].includes(member.members?.[part.parameter.name]?.kind ?? '')
                      ? `#${symbolId(`${path}.${part.parameter.name}`)}`
                      : undefined
                }
                parameter={part.parameter}
                symbols={symbols}
              />}
          {index < parts.length - 1 || multiline ? ',' : ''}
          {multiline ? '\n' : index < parts.length - 1 ? ' ' : ''}
        </span>
      ))}
      ){member.kind !== 'class' && member.returns && <> -&gt; <LinkedType annotation={member.returns} symbols={symbols} /></>}
    </Signature>
  );
}

function ValueSignature({name, member, symbols}: {name: string; member: Member; symbols: SymbolIndex}): ReactNode {
  return (
    <>
      {name}
      {member.annotation && <>: <LinkedType annotation={member.annotation} symbols={symbols} /></>}
      {member.value !== undefined && <> = {member.value}</>}
    </>
  );
}

function parseDocstring(value: string): DocBlock[] {
  const blocks: DocBlock[] = [];
  const paragraph: string[] = [];
  let code: string[] | undefined;
  let language: string | undefined;
  let currentSection: string | undefined;

  const flushParagraph = () => {
    if (paragraph.length === 0) return;
    blocks.push({kind: 'paragraph', section: currentSection, value: paragraph.join(' ')});
    paragraph.length = 0;
  };

  for (const line of value.split('\n')) {
    const trimmed = line.trim();
    if (trimmed.startsWith('```')) {
      if (code) {
        blocks.push({kind: 'code', language, section: currentSection, value: code.join('\n')});
        code = undefined;
        language = undefined;
      } else {
        flushParagraph();
        code = [];
        language = trimmed.slice(3) || undefined;
      }
      continue;
    }
    if (code) {
      code.push(line);
      continue;
    }
    if (!trimmed) {
      flushParagraph();
      continue;
    }

    const indentation = line.length - line.trimStart().length;
    if (indentation === 0 && /^[A-Za-z][A-Za-z ]+:$/.test(trimmed)) {
      flushParagraph();
      currentSection = trimmed.slice(0, -1);
      blocks.push({kind: 'section', value: currentSection});
    } else if (indentation > 0 && /^[A-Za-z_][\w. -]*:\s*.+$/.test(trimmed)) {
      flushParagraph();
      const separator = trimmed.indexOf(':');
      blocks.push({
        kind: 'field',
        name: trimmed.slice(0, separator),
        description: trimmed.slice(separator + 1).trim(),
        section: currentSection,
      });
    } else {
      paragraph.push(trimmed);
    }
  }
  flushParagraph();
  if (code) blocks.push({kind: 'code', language, section: currentSection, value: code.join('\n')});
  return blocks;
}

function Docstring({value, omitSections = [], path}: {value?: string; omitSections?: string[]; path?: string}): ReactNode {
  if (!value) return null;
  const blocks = parseDocstring(value).filter((block) => {
    const section = block.kind === 'section' ? block.value : block.section;
    return !section || !omitSections.includes(section);
  });
  return (
    <div className={styles.docstring}>
      {blocks.map((block, index) => {
        if (block.kind === 'code') {
          return (
            <CodeBlock key={index} language={block.language ?? 'text'}>
              {block.value}
            </CodeBlock>
          );
        }
        if (block.kind === 'section') return <h5 key={index}>{block.value}</h5>;
        if (block.kind === 'field') {
          const id = path && parameterSections.has(block.section ?? '')
            ? parameterId(path, block.name)
            : undefined;
          return (
            <div className={styles.docField} id={id} key={index}>
              <span className={styles.targetLabel}>
                <code>{block.name}</code>
                {id && <TargetMarker id={id} label={`parameter ${block.name}`} />}
              </span>
              <span>{block.description}</span>
            </div>
          );
        }
        return <p key={index}>{block.value}</p>;
      })}
    </div>
  );
}

function Signature({children}: {children: ReactNode}): ReactNode {
  return (
    <div className={styles.signature}>
      <pre><code>{children}</code></pre>
    </div>
  );
}

function docFields(value: string | undefined, section: string): Map<string, string> {
  if (!value) return new Map();
  return new Map(
    parseDocstring(value)
      .filter((block): block is Extract<DocBlock, {kind: 'field'}> =>
        block.kind === 'field' && block.section === section,
      )
      .map((block) => [block.name, block.description]),
  );
}

function AttributeTable({members, doc, path, symbols}: {members: NamedMember[]; doc?: string; path: string; symbols: SymbolIndex}): ReactNode {
  if (members.length === 0) return null;
  const descriptions = docFields(doc, 'Attributes');
  return (
    <section className={styles.memberGroup}>
      <h4>Attributes</h4>
      <div className={styles.tableScroll}>
        <table className={styles.attributeTable}>
          <thead>
            <tr><th>Name</th><th>Type</th><th>Default</th><th>Description</th></tr>
          </thead>
          <tbody>
            {members.map(([name, member]) => {
              const id = symbolId(`${path}.${name}`);
              return (
                <tr id={id} key={name}>
                  <td><span className={styles.targetLabel}><code>{name}</code><TargetMarker id={id} label={`attribute ${name}`} /></span></td>
                  <td>{member.annotation ? <code><LinkedType annotation={member.annotation} symbols={symbols} /></code> : '—'}</td>
                  <td>{member.value !== undefined ? <code>{member.value}</code> : '—'}</td>
                  <td>{member.doc || descriptions.get(name) || '—'}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function MethodReference({name, member, path, symbols}: {name: string; member: Member; path: string; symbols: SymbolIndex}): ReactNode {
  const id = symbolId(path);
  const decorators = member.decorators?.filter((item) =>
    ['classmethod', 'staticmethod'].includes(item),
  );
  const labels = [...(member.async ? ['async'] : []), ...(decorators ?? [])];
  return (
    <article className={styles.method} id={id}>
      <h5>
        <span className={styles.targetLabel}><code>{name}</code><TargetMarker id={id} label={`method ${name}`} /></span>
        {labels.map((label) => <span className={styles.memberLabel} key={label}>{label}</span>)}
      </h5>
      <CallableSignature name={name} member={member} path={path} symbols={symbols} />
      <Docstring path={path} value={member.doc} />
    </article>
  );
}

function ClassMembers({member, path, symbols}: {member: Member; path: string; symbols: SymbolIndex}): ReactNode {
  const children = Object.entries(member.members ?? {});
  const attributes = children.filter(([, child]) =>
    child.kind === 'attribute' || child.kind === 'property',
  );
  const methods = children.filter(([, child]) => child.kind === 'function');
  const nested = children.filter(([, child]) =>
    child.kind !== 'attribute' && child.kind !== 'property' && child.kind !== 'function',
  );

  return (
    <>
      <AttributeTable members={attributes} doc={member.doc} path={path} symbols={symbols} />
      {methods.length > 0 && (
        <section className={styles.memberGroup}>
          <h4>Methods</h4>
          {methods.map(([name, child]) => (
            <MethodReference key={name} name={name} member={child} path={`${path}.${name}`} symbols={symbols} />
          ))}
        </section>
      )}
      {nested.length > 0 && (
        <section className={styles.memberGroup}>
          <h4>Members</h4>
          {nested.map(([name, child]) => {
            const id = symbolId(`${path}.${name}`);
            return (
              <div className={styles.nestedMember} id={id} key={name}>
                <span className={styles.targetLabel}>
                  <code><ValueSignature name={name} member={child} symbols={symbols} /></code>
                  <TargetMarker id={id} label={`member ${name}`} />
                </span>
                <Docstring value={child.doc} />
              </div>
            );
          })}
        </section>
      )}
    </>
  );
}

function SymbolReference({moduleName, name, member, symbols}: {moduleName: string; name: string; member: Member; symbols: SymbolIndex}): ReactNode {
  const path = `${moduleName}.${name}`;
  const id = symbolId(path);
  const isCallable = member.kind === 'class' || member.kind === 'function';
  const kind = `${member.async ? 'async ' : ''}${member.kind.replace('_', ' ')}`;

  return (
    <article className={styles.symbol} id={id}>
      <header className={styles.symbolHeading}>
        <h3>
          <span className={styles.kind}>{kind}</span>
          <span className={styles.targetLabel}><code>{name}</code><TargetMarker id={id} label={`${kind} ${name}`} /></span>
        </h3>
        <code className={styles.qualifiedName}>{path}</code>
      </header>
      {isCallable
        ? <CallableSignature name={name} member={member} path={path} symbols={symbols} />
        : <Signature><ValueSignature name={name} member={member} symbols={symbols} /></Signature>}
      {member.bases && member.bases.length > 0 && (
        <p className={styles.bases}>Bases: <code>{member.bases.map((base, index) => (
          <span key={base.text}>{index > 0 && ', '}<LinkedType annotation={base} symbols={symbols} /></span>
        ))}</code></p>
      )}
      <Docstring path={path} value={member.doc} omitSections={member.kind === 'class' ? ['Attributes'] : []} />
      {member.kind === 'class' && <ClassMembers member={member} path={path} symbols={symbols} />}
    </article>
  );
}

function SymbolGroup({title, moduleName, members, symbols}: {title: string; moduleName: string; members: NamedMember[]; symbols: SymbolIndex}): ReactNode {
  if (members.length === 0) return null;
  return (
    <section className={styles.symbolGroup}>
      <h3 className={styles.groupTitle}>{title}</h3>
      {members.map(([name, member]) => (
        <SymbolReference key={name} moduleName={moduleName} name={name} member={member} symbols={symbols} />
      ))}
    </section>
  );
}

function tocKind(member: Member, nested = false): string {
  if (nested && member.kind === 'function') return 'method';
  return member.kind;
}

function tocLetter(kind: string): string {
  return {
    attribute: 'a',
    class: 'c',
    function: 'f',
    method: 'm',
    property: 'p',
    type_alias: 't',
  }[kind] ?? '·';
}

function ReferenceToc({modules}: {modules: Array<[string, ApiModule]>}): ReactNode {
  return (
    <aside className={styles.toc}>
      <nav aria-label="On this page">
        <p className={styles.tocTitle}>On this page</p>
        {modules.map(([moduleName, module]) => (
          <div className={styles.tocModule} key={moduleName}>
            <a className={styles.tocModuleName} href={`#${symbolId(moduleName)}`}>{moduleName}</a>
            <ul>
              {Object.entries(module.members).map(([name, member]) => {
                const path = `${moduleName}.${name}`;
                const children = Object.entries(member.members ?? {});
                return (
                  <li key={name}>
                    <div className={styles.tocEntry}>
                      <span className={styles.tocMarker} data-kind={tocKind(member)}>{tocLetter(tocKind(member))}</span>
                      <a href={`#${symbolId(path)}`}>{name}</a>
                    </div>
                    {children.length > 0 && (
                      <ul>
                        {children.map(([childName, child]) => {
                          const kind = tocKind(child, true);
                          return (
                            <li className={styles.tocEntry} key={childName}>
                              <span className={styles.tocMarker} data-kind={kind}>{tocLetter(kind)}</span>
                              <a href={`#${symbolId(`${path}.${childName}`)}`}>{childName}</a>
                            </li>
                          );
                        })}
                      </ul>
                    )}
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </nav>
    </aside>
  );
}

export default function PythonApiReference({data, links, view}: {data: ApiViewModel; links: Record<string, string>; view: View}): ReactNode {
  if (data.view !== view) throw new Error(`Expected Python ${view} API data, received ${data.view}`);
  const config = views[view];
  const modules = useMemo(() => Object.entries(data.modules), [data.modules]);
  const symbols = new Map([...Object.entries(links), ...buildSymbolIndex(data)]);
  const toc = useMemo(() => <ReferenceToc modules={modules} />, [modules]);

  return (
    <div className={styles.reference}>
      <DocRightSidebar>{toc}</DocRightSidebar>
      <div className={styles.content}>
        <header className={styles.pageHeader}>
          <p className={styles.eyebrow}>Python API Reference</p>
          <h1>{config.title}</h1>
          <p>{config.description}</p>
        </header>

        <nav className={styles.views} aria-label="Python API Reference sections">
          {viewLinks.map((item) => (
            <Link className={item.view === view ? styles.activeView : undefined} key={item.view} to={item.to}>
              {item.label}
            </Link>
          ))}
        </nav>

        {modules.length > 1 && (
          <nav className={styles.moduleIndex} aria-label="Modules on this page">
            <span>Modules</span>
            {modules.map(([name]) => <a href={`#${symbolId(name)}`} key={name}>{name}</a>)}
          </nav>
        )}

        {modules.map(([moduleName, module]) => {
          const id = symbolId(moduleName);
          const members = Object.entries(module.members);
          const classes = members.filter(([, member]) => member.kind === 'class');
          const functions = members.filter(([, member]) => member.kind === 'function');
          const values = members.filter(([, member]) => member.kind !== 'class' && member.kind !== 'function');
          return (
            <section className={styles.module} id={id} key={moduleName}>
              <header className={styles.moduleHeading}>
                <p>Module</p>
                <h2><span className={styles.targetLabel}><code>{moduleName}</code><TargetMarker id={id} label={`module ${moduleName}`} /></span></h2>
              </header>
              <Docstring value={module.doc} />
              <SymbolGroup title="Functions" moduleName={moduleName} members={functions} symbols={symbols} />
              <SymbolGroup title="Classes" moduleName={moduleName} members={classes} symbols={symbols} />
              <SymbolGroup title="Data" moduleName={moduleName} members={values} symbols={symbols} />
            </section>
          );
        })}
      </div>
    </div>
  );
}
