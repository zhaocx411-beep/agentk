import {execFile} from 'node:child_process';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {promisify} from 'node:util';

const run = promisify(execFile);
const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const outputDir = path.join(rootDir, 'api-reference', 'java');
const typeJsonDir = path.join(outputDir, 'types');
const typeMdxDir = path.join(rootDir, 'docs', 'reference', 'java', 'types');
const modules = ['protocol', 'client', 'client-grpc'];
const clientPackages = new Map([
  ['agentk-client', new Set(['com.kuaishou.klingai.agentk.client'])],
  ['agentk-client-grpc', new Set(['com.kuaishou.klingai.agentk.client.grpc'])],
]);

const protobufRuntimeMethods = new Set([
  'equals',
  'getDefaultInstance',
  'getDefaultInstanceForType',
  'getDescriptor',
  'getDescriptorForType',
  'getParserForType',
  'getSerializedSize',
  'getUnknownFields',
  'hashCode',
  'isInitialized',
  'parseDelimitedFrom',
  'parseFrom',
  'parser',
  'writeTo',
]);

function parseSdkPath(args) {
  const index = args.indexOf('--sdk');
  if (index < 0 || !args[index + 1]) {
    throw new Error(
      'usage: node scripts/generate-java-api-reference.mjs --sdk /path/to/agentk-sdk',
    );
  }
  return path.resolve(args[index + 1]);
}

function viewForArtifact(artifactId) {
  if (artifactId === 'agentk-protocol') return 'protocol';
  if (artifactId === 'agentk-client' || artifactId === 'agentk-client-grpc') {
    return 'client';
  }
  throw new Error(`Unsupported Java API reference artifact: ${artifactId}`);
}

function symbolKey(symbol) {
  const artifact = symbol.artifact?.artifactId ?? '';
  const owner = `${symbol.packageName}.${symbol.classNames.join('.')}`;
  const callable = symbol.callable
    ? `#${symbol.callable.name}(${symbol.callable.parameterTypes.join(',')})`
    : '';
  return `${artifact}:${owner}${callable}`;
}

function symbolAnchor(symbol) {
  return symbolKey(symbol)
    .replace(/[^a-zA-Z0-9_-]/g, '-')
    .replace(/-+/g, '-');
}

function symbolLabel(symbol) {
  const owner = [...symbol.classNames];
  if (symbol.callable) owner.push(symbol.callable.name);
  return owner.join('.');
}

function packagePath(packageName) {
  return packageName.replaceAll('.', '/');
}

function typeBaseName(symbol) {
  return symbol.classNames.join('.');
}

function typeRoute(view, artifactId, symbol) {
  return [
    '/reference/java',
    view,
    artifactId,
    packagePath(symbol.packageName),
    typeBaseName(symbol),
  ].join('/');
}

function typeRelativePath(page, extension) {
  return path.join(
    page.artifact.artifactId,
    ...page.package.name.split('.'),
    `${page.classNames.join('.')}.${extension}`,
  );
}

function externalHref(symbol) {
  const packageNamePath = packagePath(symbol.packageName);
  const classPath = symbol.classNames.join('.');
  if (!packageNamePath || !classPath) return undefined;

  if (!symbol.artifact && symbol.packageName.startsWith('java.')) {
    const javaModule = symbol.packageName.startsWith('java.sql') ? 'java.sql' : 'java.base';
    return `https://docs.oracle.com/en/java/javase/11/docs/api/${javaModule}/${packageNamePath}/${classPath}.html`;
  }
  if (symbol.artifact) {
    const {groupId, artifactId} = symbol.artifact;
    return `https://javadoc.io/doc/${groupId}/${artifactId}/latest/${packageNamePath}/${classPath}.html`;
  }
  return undefined;
}

function symbolHref(symbol, targets, localArtifactIds) {
  const local = targets.get(symbolKey(symbol));
  if (local) return local;
  if (localArtifactIds.has(symbol.artifact?.artifactId)) return undefined;
  return externalHref(symbol);
}

function isGeneratedProtobuf(type) {
  return /\bGenerated(Message|File)\b/.test(type.declaration.text);
}

function filterProtocolType(type) {
  if (type.name.endsWith('OrBuilder')) return undefined;
  if (/\bextends GeneratedFile\b/.test(type.declaration.text)) return undefined;
  if (type.name === 'Builder' && /\bGeneratedMessage\b/.test(type.declaration.text)) {
    return undefined;
  }

  const generated = isGeneratedProtobuf(type);
  return {
    ...type,
    nestedTypes: type.nestedTypes
      .map(filterProtocolType)
      .filter(Boolean),
    fields: type.fields.filter(
      (field) => !field.name.endsWith('_FIELD_NUMBER') && field.name !== 'descriptor',
    ),
    methods: type.methods.filter((method) =>
      !generated || (
        !protobufRuntimeMethods.has(method.name) &&
        !method.name.endsWith('OrBuilder') &&
        !method.name.endsWith('OrBuilderList')
      ),
    ),
  };
}

function filterArtifact(artifact) {
  const protocol = artifact.artifactId === 'agentk-protocol';
  const includedClientPackages = clientPackages.get(artifact.artifactId);
  return {
    ...artifact,
    packages: artifact.packages
      .filter((pkg) =>
        includedClientPackages
          ? includedClientPackages.has(pkg.name)
          : !pkg.name.split('.').includes('internal'),
      )
      .map((pkg) => ({
        ...pkg,
        types: protocol
          ? pkg.types.map(filterProtocolType).filter(Boolean)
          : pkg.types,
      }))
      .filter((pkg) => pkg.types.length > 0),
  };
}

function collectTypePages(pages, view, artifact, pkg, type) {
  if (!type.id || !Array.isArray(type.id.classNames) || type.id.classNames.length === 0) {
    throw new Error(`Java API reference type ${pkg.name}.${type.name} has no canonical identity`);
  }

  const page = {
    view,
    artifact,
    package: pkg,
    type,
    classNames: type.id.classNames,
    route: typeRoute(view, artifact.artifactId, type.id),
  };
  pages.push(page);

  for (const nested of type.nestedTypes) {
    collectTypePages(pages, view, artifact, pkg, nested);
  }
}

function buildTypePages(artifacts) {
  const pages = [];
  for (const artifact of artifacts) {
    const view = viewForArtifact(artifact.artifactId);
    for (const pkg of artifact.packages) {
      for (const type of pkg.types) {
        collectTypePages(pages, view, artifact, pkg, type);
      }
    }
  }
  return pages.sort((left, right) => left.route.localeCompare(right.route));
}

function setTarget(targets, symbol, href) {
  const key = symbolKey(symbol);
  const existing = targets.get(key);
  if (existing && existing !== href) {
    throw new Error(`Java API reference symbol ${key} maps to both ${existing} and ${href}`);
  }
  targets.set(key, href);
}

function buildSymbolTargets(pages) {
  const targets = new Map();
  for (const page of pages) {
    setTarget(targets, page.type.id, page.route);
    for (const member of [
      ...page.type.enumConstants,
      ...page.type.fields,
      ...page.type.constructors,
      ...page.type.methods,
    ]) {
      setTarget(targets, member.id, `${page.route}#${symbolAnchor(member.id)}`);
    }
  }
  return targets;
}

function compactAnnotatedText(value, targets, localArtifactIds) {
  return {
    text: value.text,
    references: value.references.flatMap((reference) => {
      const href = symbolHref(reference.target, targets, localArtifactIds);
      return href
        ? [{start: reference.start, end: reference.end, href}]
        : [];
    }),
  };
}

function compactDocNode(node, targets, localArtifactIds) {
  const children = node.children.flatMap((child) =>
    compactDocNode(child, targets, localArtifactIds),
  );
  if (node.kind === 'MARKDOWN_FILE' && !node.text && !node.target) return children;

  const compact = {kind: node.kind};
  if (node.text) compact.text = node.text;
  if (node.name && node.name !== node.kind) compact.name = node.name;
  if (node.target) {
    const href = symbolHref(node.target, targets, localArtifactIds);
    if (href) compact.href = href;
  }
  if (Object.keys(node.attributes).length > 0) compact.attributes = node.attributes;
  if (children.length > 0) compact.children = children;
  return [compact];
}

function compactDocumentation(documentation, targets, localArtifactIds) {
  return documentation.children.flatMap((node) =>
    compactDocNode(node, targets, localArtifactIds),
  );
}

function docText(node) {
  return `${node.text ?? ''}${node.children.map(docText).join('')}`;
}

function documentationSummary(documentation) {
  const description = documentation.children.find((node) => node.kind === 'Description');
  const value = (description ? docText(description) : '')
    .replace(/\s+/g, ' ')
    .trim();
  const sentence = value.match(/^.*?(?:[.!?](?:\s|$)|$)/)?.[0] ?? value;
  return sentence.trim();
}

function compactMember(member, targets, localArtifactIds) {
  return {
    id: symbolAnchor(member.id),
    name: member.name,
    declaration: compactAnnotatedText(member.declaration, targets, localArtifactIds),
    documentation: compactDocumentation(member.documentation, targets, localArtifactIds),
  };
}

function appendAnnotatedText(target, value) {
  const offset = target.text.length;
  target.text += value.text;
  target.references.push(...value.references.map((reference) => ({
    ...reference,
    start: reference.start + offset,
    end: reference.end + offset,
  })));
}

function compactCallableSignature(member, targets, localArtifactIds) {
  const signature = {text: `${member.name}(`, references: []};
  member.parameters.forEach((parameter, index) => {
    if (index > 0) signature.text += ', ';
    const type = compactAnnotatedText(
      parameter.type.declaration,
      targets,
      localArtifactIds,
    );
    appendAnnotatedText(signature, type);
    if (parameter.name) signature.text += ` ${parameter.name}`;
  });
  signature.text += ')';
  return signature;
}

function compactSummaryType(member, ownerKind, targets, localArtifactIds) {
  const summary = {text: '', references: []};
  const modifiers = member.modifiers.filter(
    (modifier) =>
      modifier !== 'public' &&
      !(ownerKind === 'interface' && modifier === 'abstract'),
  );
  summary.text = modifiers.join(' ');

  if (member.typeParameters.length > 0) {
    if (summary.text) summary.text += ' ';
    summary.text += '<';
    member.typeParameters.forEach((parameter, index) => {
      if (index > 0) summary.text += ', ';
      summary.text += parameter.name;
      if (parameter.bounds.length > 0) {
        summary.text += ' extends ';
        parameter.bounds.forEach((bound, boundIndex) => {
          if (boundIndex > 0) summary.text += ' & ';
          appendAnnotatedText(
            summary,
            compactAnnotatedText(bound.declaration, targets, localArtifactIds),
          );
        });
      }
    });
    summary.text += '>';
  }

  if (member.returnType) {
    if (summary.text) summary.text += ' ';
    appendAnnotatedText(
      summary,
      compactAnnotatedText(member.returnType.declaration, targets, localArtifactIds),
    );
  }
  return summary;
}

function compactExecutable(member, ownerKind, targets, localArtifactIds) {
  return {
    ...compactMember(member, targets, localArtifactIds),
    ...(member.returnType
      ? {summaryType: compactSummaryType(member, ownerKind, targets, localArtifactIds)}
      : {}),
    summarySignature: compactCallableSignature(member, targets, localArtifactIds),
  };
}

function compactInheritedGroup(group, targets, localArtifactIds) {
  const declaringHref = symbolHref(group.declaringType, targets, localArtifactIds);
  return {
    declaringType: symbolLabel(group.declaringType),
    ...(declaringHref ? {declaringHref} : {}),
    members: [...group.fields, ...group.methods].map((member) => {
      const href = symbolHref(member, targets, localArtifactIds);
      return {
        name: symbolLabel(member),
        ...(href ? {href} : {}),
      };
    }),
  };
}

function compactTypeLink(type, targets) {
  const href = targets.get(symbolKey(type.id));
  if (!href) {
    throw new Error(`Java API reference type ${symbolKey(type.id)} has no page`);
  }
  return {
    kind: type.kind,
    name: type.name,
    href,
    summary: documentationSummary(type.documentation),
  };
}

function compactTypePage(type, targets, localArtifactIds) {
  return {
    id: symbolAnchor(type.id),
    kind: type.kind,
    name: type.name,
    declaration: compactAnnotatedText(type.declaration, targets, localArtifactIds),
    documentation: compactDocumentation(type.documentation, targets, localArtifactIds),
    nestedTypes: type.nestedTypes.map((nested) => compactTypeLink(nested, targets)),
    enumConstants: type.enumConstants.map((member) =>
      compactMember(member, targets, localArtifactIds),
    ),
    fields: type.fields.map((member) =>
      compactMember(member, targets, localArtifactIds),
    ),
    constructors: type.constructors.map((member) =>
      compactExecutable(member, type.kind, targets, localArtifactIds),
    ),
    methods: type.methods.map((member) =>
      compactExecutable(member, type.kind, targets, localArtifactIds),
    ),
    inheritedMembers: type.inheritedMembers
      .map((group) => compactInheritedGroup(group, targets, localArtifactIds))
      .filter((group) => group.members.length > 0),
  };
}

function createIndex(view, artifacts, targets, localArtifactIds) {
  return {
    view,
    artifacts: artifacts
      .filter((artifact) => viewForArtifact(artifact.artifactId) === view)
      .map((artifact) => ({
        groupId: artifact.groupId,
        artifactId: artifact.artifactId,
        packages: artifact.packages.map((pkg) => ({
          name: pkg.name,
          documentation: compactDocumentation(pkg.documentation, targets, localArtifactIds),
          types: pkg.types.map((type) => compactTypeLink(type, targets)),
        })),
      })),
  };
}

function createTypeModel(page, targets, localArtifactIds) {
  return {
    view: page.view,
    artifact: {
      groupId: page.artifact.groupId,
      artifactId: page.artifact.artifactId,
    },
    packageName: page.package.name,
    classNames: page.classNames,
    type: compactTypePage(page.type, targets, localArtifactIds),
  };
}

function renderTypeMdx(page) {
  const jsonRelativePath = path.posix.join(
    'types',
    ...typeRelativePath(page, 'json').split(path.sep),
  );
  const title = page.classNames.join('.');
  const description = documentationSummary(page.type.documentation) ||
    `${page.package.name}.${title} Java API reference`;

  return `---
title: ${JSON.stringify(title)}
description: ${JSON.stringify(description)}
slug: ${page.route}
hide_title: true
hide_table_of_contents: true
custom_table_of_contents: true
displayed_sidebar: referenceSidebar
pagination_prev: null
pagination_next: null
---

import {JavaTypeReference} from '@site/src/components/reference/JavaApiReference';
import data from '@site/api-reference/java/${jsonRelativePath}';

<JavaTypeReference data={data} />
`;
}

function assertUniqueOutputs(pages) {
  const routes = new Set();
  const jsonPaths = new Set();
  const mdxPaths = new Set();
  for (const page of pages) {
    const jsonPath = typeRelativePath(page, 'json');
    const mdxPath = typeRelativePath(page, 'mdx');
    for (const [set, value, kind] of [
      [routes, page.route, 'route'],
      [jsonPaths, jsonPath, 'JSON path'],
      [mdxPaths, mdxPath, 'MDX path'],
    ]) {
      if (set.has(value)) {
        throw new Error(`Duplicate Java API reference ${kind}: ${value}`);
      }
      set.add(value);
    }
  }
}

async function writeJson(file, value) {
  await fs.mkdir(path.dirname(file), {recursive: true});
  await fs.writeFile(file, `${JSON.stringify(value, null, 2)}\n`, 'utf8');
}

async function writeOutputs(indexes, pages, targets, localArtifactIds) {
  await Promise.all([
    fs.rm(typeJsonDir, {recursive: true, force: true}),
    fs.rm(typeMdxDir, {recursive: true, force: true}),
  ]);

  const writes = [
    writeJson(path.join(outputDir, 'client.json'), indexes.client),
    writeJson(path.join(outputDir, 'protocol.json'), indexes.protocol),
  ];
  for (const page of pages) {
    const jsonFile = path.join(typeJsonDir, typeRelativePath(page, 'json'));
    const mdxFile = path.join(typeMdxDir, typeRelativePath(page, 'mdx'));
    writes.push(writeJson(jsonFile, createTypeModel(page, targets, localArtifactIds)));
    writes.push((async () => {
      await fs.mkdir(path.dirname(mdxFile), {recursive: true});
      await fs.writeFile(mdxFile, renderTypeMdx(page), 'utf8');
    })());
  }
  await Promise.all(writes);
}

async function generateFragment(javaRoot, temporaryDir, module) {
  const output = path.join(temporaryDir, `${module}.json`);
  await run(
    'mvn',
    [
      '-q',
      '-f',
      `${module}/pom.xml`,
      '-Papiref',
      `-Dapiref.output=${output}`,
      'dokka:dokka',
    ],
    {cwd: javaRoot, maxBuffer: 16 * 1024 * 1024},
  );
  const artifact = JSON.parse(await fs.readFile(output, 'utf8'));
  if (!artifact.artifactId || !Array.isArray(artifact.packages)) {
    throw new Error(`Java API reference generator returned an invalid ${module} fragment`);
  }
  return filterArtifact(artifact);
}

async function main() {
  const sdkRoot = parseSdkPath(process.argv.slice(2));
  const javaRoot = path.join(sdkRoot, 'java');
  await fs.access(path.join(javaRoot, 'apiref', 'pom.xml'));

  const temporaryDir = await fs.mkdtemp(
    path.join(os.tmpdir(), 'agentk-java-api-reference-'),
  );
  try {
    await run(
      'mvn',
      [
        '-q',
        '-DskipTests',
        '-pl',
        'apiref,protocol,client,client-grpc',
        '-am',
        'install',
      ],
      {cwd: javaRoot, maxBuffer: 16 * 1024 * 1024},
    );

    const artifacts = await Promise.all(
      modules.map((module) => generateFragment(javaRoot, temporaryDir, module)),
    );
    const pages = buildTypePages(artifacts);
    assertUniqueOutputs(pages);

    const targets = buildSymbolTargets(pages);
    const localArtifactIds = new Set(artifacts.map((artifact) => artifact.artifactId));
    const indexes = {
      client: createIndex('client', artifacts, targets, localArtifactIds),
      protocol: createIndex('protocol', artifacts, targets, localArtifactIds),
    };

    await writeOutputs(indexes, pages, targets, localArtifactIds);
  } finally {
    await fs.rm(temporaryDir, {recursive: true, force: true});
  }
}

await main();
