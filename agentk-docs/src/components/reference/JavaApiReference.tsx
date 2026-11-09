import Link from '@docusaurus/Link';
import {useMemo, type ReactNode} from 'react';
import {DocRightSidebar} from '../layout/DocRightSidebar';
import styles from './JavaApiReference.module.css';

type TextReference = {
  start: number;
  end: number;
  href: string;
};

type AnnotatedText = {
  text: string;
  references: TextReference[];
};

type DocNode = {
  kind: string;
  text?: string;
  name?: string;
  href?: string;
  attributes?: Record<string, string>;
  children?: DocNode[];
};

type MemberDoc = {
  id: string;
  name: string;
  summaryType?: AnnotatedText;
  summarySignature?: AnnotatedText;
  declaration: AnnotatedText;
  documentation: DocNode[];
};

type InheritedMemberGroup = {
  declaringType: string;
  declaringHref?: string;
  members: Array<{name: string; href?: string}>;
};

type TypeKind = 'annotation' | 'class' | 'enum' | 'interface';

type TypeLink = {
  kind: TypeKind;
  name: string;
  href: string;
  summary: string;
};

type TypeDoc = {
  id: string;
  kind: TypeKind;
  name: string;
  declaration: AnnotatedText;
  documentation: DocNode[];
  nestedTypes: TypeLink[];
  enumConstants: MemberDoc[];
  fields: MemberDoc[];
  constructors: MemberDoc[];
  methods: MemberDoc[];
  inheritedMembers: InheritedMemberGroup[];
};

type PackageDoc = {
  name: string;
  documentation: DocNode[];
  types: TypeLink[];
};

type ArtifactDoc = {
  groupId: string;
  artifactId: string;
  packages: PackageDoc[];
};

export type View = 'client' | 'protocol';

export type ApiViewModel = {
  view: View;
  artifacts: ArtifactDoc[];
};

export type TypePageModel = {
  view: View;
  artifact: {
    groupId: string;
    artifactId: string;
  };
  packageName: string;
  classNames: string[];
  type: TypeDoc;
};

const views: Record<View, {title: string; description: string}> = {
  client: {
    title: 'Java Client API',
    description: '创建和管理 Session、启动 Execution，并通过 KESS gRPC 连接 AgentK。KS Boot artifact 提供相同的公开 Client API。',
  },
  protocol: {
    title: 'Java Protocol Types',
    description: 'Java SDK 暴露的 protobuf message、enum 与 Connect service 类型。',
  },
};

const viewLinks: Array<{view: View; label: string; to: string}> = [
  {view: 'client', label: 'Client', to: '/reference/java/client'},
  {view: 'protocol', label: 'Protocol', to: '/reference/java/protocol'},
];

function packageId(name: string): string {
  return `java-package-${name.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
}

function declarationText(value: string): string {
  return value.replace(/\)(?=(?:public|protected)\b)/g, ')\n');
}

function AnnotatedCode({value}: {value: AnnotatedText}): ReactNode {
  const rendered: ReactNode[] = [];
  let offset = 0;
  value.references.forEach((reference, index) => {
    if (
      reference.start < offset ||
      reference.end < reference.start ||
      reference.end > value.text.length
    ) return;
    rendered.push(declarationText(value.text.slice(offset, reference.start)));
    rendered.push(
      <a href={reference.href} key={`${reference.start}:${reference.end}:${index}`}>
        {value.text.slice(reference.start, reference.end)}
      </a>,
    );
    offset = reference.end;
  });
  rendered.push(declarationText(value.text.slice(offset)));
  return rendered;
}

function Declaration({value}: {value: AnnotatedText}): ReactNode {
  return (
    <div className={styles.declaration}>
      <pre><code><AnnotatedCode value={value} /></code></pre>
    </div>
  );
}

function plainText(nodes: DocNode[] = []): string {
  return nodes.map((node) => node.text ?? plainText(node.children)).join('');
}

function DocContent({nodes}: {nodes: DocNode[]}): ReactNode {
  return nodes.map((node, index) => {
    const children = node.children ? <DocContent nodes={node.children} /> : null;
    if (node.kind === 'Text') return node.text ?? null;
    if (node.kind === 'P') return <p key={index}>{children}</p>;
    if (node.kind === 'CodeInline') return <code key={index}>{node.text ?? children}</code>;
    if (node.kind === 'Pre') return <pre key={index}><code>{plainText(node.children)}</code></pre>;
    if (node.kind === 'DocumentationLink') {
      return node.href
        ? <a href={node.href} key={index}>{children || node.text || node.name}</a>
        : <span key={index}>{children || node.text || node.name}</span>;
    }
    return <span key={index}>{node.text}{children}</span>;
  });
}

function Documentation({nodes}: {nodes: DocNode[]}): ReactNode {
  if (nodes.length === 0) return null;
  const descriptions = nodes.filter((node) => node.kind === 'Description');
  const tags = nodes.filter((node) => node.kind !== 'Description');
  return (
    <div className={styles.documentation}>
      {descriptions.map((node, index) => (
        <DocContent key={index} nodes={node.children ?? []} />
      ))}
      {tags.length > 0 && (
        <dl className={styles.docTags}>
          {tags.map((node, index) => (
            <div key={index}>
              <dt>{node.kind}{node.name ? ` ${node.name}` : ''}</dt>
              <dd><DocContent nodes={node.children ?? []} /></dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  );
}

function documentationSummary(nodes: DocNode[]): string {
  const description = nodes.find((node) => node.kind === 'Description');
  const value = (description?.children ?? [])
    .map((node) => plainText([node]).trim())
    .filter(Boolean)
    .join(' ');
  const sentence = value.match(/^.*?(?:[.!?](?:\s|$)|$)/)?.[0] ?? value;
  return sentence.trim();
}

function ViewNav({view}: {view: View}): ReactNode {
  return (
    <nav className={styles.views} aria-label="Java API Reference sections">
      {viewLinks.map((item) => (
        <Link className={item.view === view ? styles.activeView : undefined} key={item.view} to={item.to}>
          {item.label}
        </Link>
      ))}
    </nav>
  );
}

function signatureParameters(member: MemberDoc): AnnotatedText | undefined {
  if (!member.summarySignature) return undefined;
  const offset = member.name.length;
  return {
    text: member.summarySignature.text.slice(offset),
    references: member.summarySignature.references.map((reference) => ({
      ...reference,
      start: reference.start - offset,
      end: reference.end - offset,
    })),
  };
}

function CallableSignature({member}: {member: MemberDoc}): ReactNode {
  const parameters = signatureParameters(member);
  return (
    <code className={styles.summarySignature}>
      <a href={`#${member.id}`}>{member.name}</a>
      {parameters && <AnnotatedCode value={parameters} />}
    </code>
  );
}

function MemberSummary({
  members,
  title,
  variant = 'plain',
}: {
  members: MemberDoc[];
  title: string;
  variant?: 'plain' | 'constructor' | 'method';
}): ReactNode {
  if (members.length === 0) return null;
  return (
    <section className={styles.summaryGroup}>
      <h4>{title}</h4>
      <div className={styles.tableScroll}>
        <table className={`${styles.summaryTable} ${variant === 'method' ? styles.methodSummaryTable : ''} ${variant === 'constructor' ? styles.constructorSummaryTable : ''}`}>
          {variant !== 'plain' && (
            <thead>
              <tr>
                {variant === 'method' && <th scope="col">Modifier and Type</th>}
                <th scope="col">{variant === 'method' ? 'Method' : 'Constructor'}</th>
                <th scope="col">Description</th>
              </tr>
            </thead>
          )}
          <tbody>
            {members.map((member) => {
              return (
                <tr key={member.id}>
                  {variant === 'method' && (
                    <td className={styles.modifierAndType}>
                      {member.summaryType && <code><AnnotatedCode value={member.summaryType} /></code>}
                    </td>
                  )}
                  <th scope="row">
                    {variant === 'plain'
                      ? <a href={`#${member.id}`}><code>{member.name}</code></a>
                      : <CallableSignature member={member} />}
                  </th>
                  <td>{documentationSummary(member.documentation) || '—'}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function TypeSummary({types}: {types: TypeLink[]}): ReactNode {
  if (types.length === 0) return null;
  return (
    <section className={styles.summaryGroup} id="nested-types">
      <h4>Nested types</h4>
      <div className={styles.tableScroll}>
        <table className={styles.summaryTable}>
          <tbody>
            {types.map((type) => (
              <tr key={type.href}>
                <th><a href={type.href}><code>{type.name}</code></a></th>
                <td>{type.summary || type.kind}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function MemberDetails({
  members,
  title,
  sectionId,
}: {
  members: MemberDoc[];
  title: string;
  sectionId: string;
}): ReactNode {
  if (members.length === 0) return null;
  return (
    <section className={styles.detailGroup} id={sectionId}>
      <h4>{title}</h4>
      {members.map((member) => (
        <article className={styles.member} id={member.id} key={member.id}>
          <h5><a href={`#${member.id}`}><code>{member.name}</code></a></h5>
          <Declaration value={member.declaration} />
          <Documentation nodes={member.documentation} />
        </article>
      ))}
    </section>
  );
}

function InheritedMembers({groups}: {groups: InheritedMemberGroup[]}): ReactNode {
  if (groups.length === 0) return null;
  return (
    <section className={styles.inherited} id="inherited-members">
      <h4>Inherited members</h4>
      {groups.map((group) => (
        <p key={group.declaringType}>
          <span>From </span>
          {group.declaringHref
            ? <a href={group.declaringHref}><code>{group.declaringType}</code></a>
            : <code>{group.declaringType}</code>}
          {': '}
          {group.members.map((member, index) => (
            <span key={`${member.name}:${index}`}>
              {index > 0 && ', '}
              {member.href ? <a href={member.href}>{member.name}</a> : member.name}
            </span>
          ))}
        </p>
      ))}
    </section>
  );
}

function PackageReference({artifactId, pkg}: {artifactId: string; pkg: PackageDoc}): ReactNode {
  const typeGroups: Array<{kind: TypeKind; title: string}> = [
    {kind: 'interface', title: 'Interfaces'},
    {kind: 'class', title: 'Classes'},
    {kind: 'enum', title: 'Enum Classes'},
    {kind: 'annotation', title: 'Annotation Interfaces'},
  ];
  return (
    <section className={styles.package} id={packageId(pkg.name)}>
      <header className={styles.packageHeading}>
        <p>package</p>
        <h2><code>{pkg.name}</code></h2>
        <code>{artifactId}</code>
      </header>
      <Documentation nodes={pkg.documentation} />
      <div className={styles.packageSummaries}>
        {typeGroups.map((group) => {
          const types = pkg.types.filter((type) => type.kind === group.kind);
          if (types.length === 0) return null;
          return (
            <section className={styles.packageSummary} key={group.kind}>
              <h3>{group.title}</h3>
              <div className={styles.tableScroll}>
                <table className={styles.summaryTable}>
                  <tbody>
                    {types.map((type) => (
                      <tr key={type.href}>
                        <th><a href={type.href}><code>{type.name}</code></a></th>
                        <td>{type.summary || '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          );
        })}
      </div>
      <a className={styles.backToTop} href="#java-reference-top">Back to top ↑</a>
    </section>
  );
}

function IndexToc({artifacts}: {artifacts: ArtifactDoc[]}): ReactNode {
  return (
    <aside className={styles.toc}>
      <nav aria-label="On this page">
        <p>On this page</p>
        <ul>
          {artifacts.flatMap((artifact) => artifact.packages.map((pkg) => (
            <li key={`${artifact.artifactId}:${pkg.name}`}>
              <a href={`#${packageId(pkg.name)}`}>{pkg.name}</a>
            </li>
          )))}
        </ul>
      </nav>
    </aside>
  );
}

function TypePageToc({type}: {type: TypeDoc}): ReactNode {
  const sections = [
    {id: 'overview', label: 'Overview', visible: true},
    {id: 'nested-types', label: 'Nested types', visible: type.nestedTypes.length > 0},
    {id: 'enum-constants', label: 'Enum constants', visible: type.enumConstants.length > 0},
    {id: 'fields', label: 'Fields', visible: type.fields.length > 0},
    {id: 'constructors', label: 'Constructors', visible: type.constructors.length > 0},
    {id: 'methods', label: 'Methods', visible: type.methods.length > 0},
    {id: 'inherited-members', label: 'Inherited members', visible: type.inheritedMembers.length > 0},
  ];
  return (
    <aside className={styles.toc}>
      <nav aria-label="On this page">
        <p>On this page</p>
        <ul>
          {sections.filter((section) => section.visible).map((section) => (
            <li key={section.id}><a href={`#${section.id}`}>{section.label}</a></li>
          ))}
        </ul>
      </nav>
    </aside>
  );
}

function typeCount(artifacts: ArtifactDoc[]): number {
  return artifacts.reduce(
    (total, artifact) => total + artifact.packages.reduce(
      (packageTotal, pkg) => packageTotal + pkg.types.length,
      0,
    ),
    0,
  );
}

export default function JavaApiReference({data, view}: {data: ApiViewModel; view: View}): ReactNode {
  if (data.view !== view) throw new Error(`Expected Java ${view} API data, received ${data.view}`);
  const config = views[view];
  const toc = useMemo(() => <IndexToc artifacts={data.artifacts} />, [data.artifacts]);

  return (
    <div className={styles.reference} id="java-reference-top">
      <DocRightSidebar>{toc}</DocRightSidebar>
      <header className={styles.pageHeader}>
        <p>Java API Reference</p>
        <h1>{config.title}</h1>
        <div className={styles.summary}>
          <span>{data.artifacts.length} artifacts</span>
          <span>{typeCount(data.artifacts)} types</span>
        </div>
        <p>{config.description}</p>
      </header>

      <ViewNav view={view} />

      <nav className={styles.packageNav} aria-label="Packages on this page">
        <span>Packages</span>
        {data.artifacts.flatMap((artifact) => artifact.packages.map((pkg) => (
          <a href={`#${packageId(pkg.name)}`} key={`${artifact.artifactId}:${pkg.name}`}>{pkg.name}</a>
        )))}
      </nav>

      {data.artifacts.flatMap((artifact) => artifact.packages.map((pkg) => (
        <PackageReference artifactId={artifact.artifactId} key={`${artifact.artifactId}:${pkg.name}`} pkg={pkg} />
      )))}
    </div>
  );
}

export function JavaTypeReference({data}: {data: TypePageModel}): ReactNode {
  const toc = useMemo(() => <TypePageToc type={data.type} />, [data.type]);
  const indexRoute = `/reference/java/${data.view}`;
  const typeName = data.classNames.join('.');

  return (
    <div className={styles.reference}>
      <DocRightSidebar>{toc}</DocRightSidebar>
      <header className={styles.typePageHeading} id="overview">
        <p>{data.type.kind}</p>
        <h1><code>{typeName}</code></h1>
        <div className={styles.typeMeta}>
          <a href={`${indexRoute}#${packageId(data.packageName)}`}>{data.packageName}</a>
          <span>{data.artifact.artifactId}</span>
        </div>
      </header>

      <ViewNav view={data.view} />

      <Declaration value={data.type.declaration} />
      <Documentation nodes={data.type.documentation} />

      <div className={styles.summaries}>
        <TypeSummary types={data.type.nestedTypes} />
        <MemberSummary members={data.type.enumConstants} title="Enum constants" />
        <MemberSummary members={data.type.fields} title="Fields" />
        <MemberSummary members={data.type.constructors} title="Constructors" variant="constructor" />
        <MemberSummary members={data.type.methods} title="Methods" variant="method" />
      </div>

      <MemberDetails members={data.type.enumConstants} title="Enum constant details" sectionId="enum-constants" />
      <MemberDetails members={data.type.fields} title="Field details" sectionId="fields" />
      <MemberDetails members={data.type.constructors} title="Constructor details" sectionId="constructors" />
      <MemberDetails members={data.type.methods} title="Method details" sectionId="methods" />
      <InheritedMembers groups={data.type.inheritedMembers} />
    </div>
  );
}
