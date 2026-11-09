import fs from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import * as cheerio from 'cheerio';

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const buildDir = path.join(rootDir, 'build');
const docsDir = path.join(rootDir, 'docs');
const searchDocumentsFile = path.join(
  rootDir,
  'generated/search-documents.json',
);
const siteUrl = new URL(
  process.env.AGENTK_DOCS_PUBLIC_URL?.trim() ||
    'https://localhost:3000/docs/',
);
const baseUrl = siteUrl.pathname;
const forbiddenMarkers = [
  '__docusaurus',
  'theme-doc-',
  'menu__',
  'navbar',
  'pagination-nav',
  'codeBlockContainer_',
  'data-has-hydrated',
];

function assert(condition, message) {
  if (!condition) {
    throw new Error(`AI docs verification failed: ${message}`);
  }
}

async function readNonEmpty(file) {
  const content = await fs.readFile(file, 'utf8');
  assert(content.trim().length > 0, `${path.relative(rootDir, file)} is empty`);
  return content;
}

async function collectDocSources(directory) {
  const entries = await fs.readdir(directory, {withFileTypes: true});
  const files = [];
  for (const entry of entries) {
    const fullPath = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await collectDocSources(fullPath)));
    } else if (entry.name.endsWith('.md') || entry.name.endsWith('.mdx')) {
      files.push(fullPath);
    }
  }
  return files;
}

function markdownLinks(content) {
  return [...content.matchAll(/\[[^\]]+\]\(([^)]+)\)/g)].map(
    (match) => match[1],
  );
}

function linkTitles(content) {
  return [...content.matchAll(/^- \[([^\]]+)\]\(([^)]+\.md)\):/gm)].map(
    (match) => match[1],
  );
}

function stripBaseUrl(pathname) {
  const basePath = baseUrl.replace(/\/$/, '');
  if (pathname === basePath || pathname === baseUrl) {
    return '/';
  }
  if (!pathname.startsWith(`${basePath}/`)) {
    return undefined;
  }
  return pathname.slice(basePath.length) || '/';
}

function logicalPathname(pathname) {
  return stripBaseUrl(pathname) ?? pathname;
}

function assertPublicPath(url, label) {
  const parsed = new URL(url, siteUrl.origin);
  assert(
    parsed.origin !== siteUrl.origin || stripBaseUrl(parsed.pathname) !== undefined,
    `${label} is outside ${baseUrl}: ${url}`,
  );
}

function outputPathForUrl(url) {
  const pathname = new URL(url, 'https://agentk.invalid').pathname;
  return path.join(buildDir, logicalPathname(pathname).replace(/^\/+/, ''));
}

function toPageUrl(route) {
  return new URL(route === '/' ? '' : route.replace(/^\//, ''), siteUrl).href;
}

async function htmlFileForMarkdown(markdownUrl) {
  const pathname = logicalPathname(
    new URL(markdownUrl, 'https://agentk.invalid').pathname,
  );
  const route =
    pathname === '/index.md' ? '' : pathname.replace(/\.md$/, '').slice(1);
  const candidates =
    route === ''
      ? [path.join(buildDir, 'index.html')]
      : [
          path.join(buildDir, route, 'index.html'),
          path.join(buildDir, `${route}.html`),
        ];
  for (const candidate of candidates) {
    try {
      await fs.access(candidate);
      return candidate;
    } catch {
      // Try the next Docusaurus output shape.
    }
  }
  throw new Error(`No HTML page found for ${markdownUrl}`);
}

const llmsFile = path.join(buildDir, 'llms.txt');
const llmsFullFile = path.join(buildDir, 'llms-full.txt');
const llms = await readNonEmpty(llmsFile);
const llmsFull = await readNonEmpty(llmsFullFile);
const docLinks = markdownLinks(llms).filter((link) =>
  new URL(link, 'https://agentk.invalid').pathname.endsWith('.md'),
);
const uniqueDocLinks = [...new Set(docLinks)];
const sourceDocs = await collectDocSources(docsDir);
const searchDocuments = JSON.parse(await readNonEmpty(searchDocumentsFile));
const pageUrls = new Set(
  searchDocuments.pages.map((page) => toPageUrl(page.route)),
);

assert(
  uniqueDocLinks.length === sourceDocs.length,
  `generated ${uniqueDocLinks.length} Markdown pages for ${sourceDocs.length} docs sources`,
);
assert(
  siteUrl.protocol === 'https:' && baseUrl === '/docs/',
  'AGENTK_DOCS_PUBLIC_URL must be an HTTPS URL with the exact path /docs/',
);
for (const link of markdownLinks(llms)) {
  assertPublicPath(link, 'llms.txt link');
}
assert(
  searchDocuments.format === 'agentk-docs-search-v1',
  'search-documents.json has an unknown format',
);
assert(
  Array.isArray(searchDocuments.pages) &&
    searchDocuments.pages.length === uniqueDocLinks.length,
  'search document page count differs from generated Markdown page count',
);

const searchRoutes = new Set();
const searchSectionIds = new Set();
const searchAreas = new Set([
  'intro',
  'concepts',
  'client',
  'worker',
  'reference',
  'examples',
  'capabilities',
  'platform',
]);
const searchKinds = new Set([
  'overview',
  'guide',
  'concept',
  'example',
  'api',
  'catalog',
  'platform',
]);
const searchLanguages = new Set(['go', 'python', 'java', null]);
for (const page of searchDocuments.pages) {
  assert(typeof page.route === 'string', 'search page route is missing');
  assert(!searchRoutes.has(page.route), `duplicate search route ${page.route}`);
  searchRoutes.add(page.route);
  assert(searchAreas.has(page.area), `${page.route} has invalid area`);
  assert(searchKinds.has(page.kind), `${page.route} has invalid kind`);
  assert(
    searchLanguages.has(page.language),
    `${page.route} has invalid language`,
  );
  assert(
    Array.isArray(page.sections) && page.sections.length > 0,
    `${page.route} has no search sections`,
  );
  await readNonEmpty(outputPathForUrl(page.markdownPath));
  const htmlFile = await htmlFileForMarkdown(page.markdownPath);
  const html = await readNonEmpty(htmlFile);
  const $ = cheerio.load(html);
  const canonical = $('link[rel="canonical"]').attr('href');
  assert(canonical, `${page.route} is missing its canonical link`);
  assertPublicPath(canonical, `${page.route} canonical link`);
  $('script[src], link[rel="stylesheet"][href]').each((_, element) => {
    const asset = $(element).attr('src') ?? $(element).attr('href');
    if (asset) {
      assertPublicPath(asset, `${page.route} asset`);
    }
  });
  const anchors = new Set(
    $('[id]')
      .toArray()
      .map((element) => $(element).attr('id')),
  );
  for (const section of page.sections) {
    assert(
      typeof section.id === 'string' && !searchSectionIds.has(section.id),
      `duplicate or missing search section ID ${section.id}`,
    );
    searchSectionIds.add(section.id);
    assert(
      typeof section.markdown === 'string' && section.markdown.trim(),
      `${section.id} has empty Markdown`,
    );
    assert(
      section.markdown.length <= 6000,
      `${section.id} exceeds the 6,000 character limit`,
    );
    assert(
      Array.isArray(section.headingPath) && Array.isArray(section.codeTerms),
      `${section.id} has invalid arrays`,
    );
    if (section.anchor !== null) {
      assert(
        anchors.has(section.anchor),
        `${section.id} points to missing HTML anchor ${section.anchor}`,
      );
    }
    assert(
      !/[\u200B-\u200D\uFEFF]/u.test(section.markdown),
      `${section.id} contains zero-width characters`,
    );
    assert(
      !section.markdown.includes('¤'),
      `${section.id} contains generated API anchor noise`,
    );
    for (const marker of forbiddenMarkers) {
      assert(
        !section.markdown.includes(marker),
        `${section.id} contains rendered-site noise "${marker}"`,
      );
    }
    const fences = section.markdown.match(/^ {0,3}(`{3,}|~{3,})/gmu) ?? [];
    assert(
      fences.length % 2 === 0,
      `${section.id} contains a split fenced code block`,
    );
  }
}

for (const link of markdownLinks(llms)) {
  const output = outputPathForUrl(link);
  await readNonEmpty(output);
}

for (const markdownUrl of uniqueDocLinks) {
  const markdownFile = outputPathForUrl(markdownUrl);
  const markdown = await readNonEmpty(markdownFile);
  for (const marker of forbiddenMarkers) {
    assert(
      !markdown.includes(marker),
      `${markdownUrl} contains rendered-site noise "${marker}"`,
    );
  }

  for (const link of markdownLinks(markdown)) {
    const parsed = new URL(link, 'https://agentk.invalid');
    if (parsed.origin !== siteUrl.origin) {
      continue;
    }
    parsed.hash = '';
    assert(
      pageUrls.has(parsed.href),
      `${markdownUrl} links to unknown documentation page ${link}`,
    );
  }

  const htmlFile = await htmlFileForMarkdown(markdownUrl);
  const html = await readNonEmpty(htmlFile);
  assert(
    html.includes(
      `<link rel="alternate" type="text/markdown" href="${markdownUrl}">`,
    ),
    `${markdownUrl} is missing its HTML alternate link`,
  );
}

const quickstart = await readNonEmpty(
  path.join(buildDir, 'client/quickstart.md'),
);
assert(quickstart.includes('```python'), 'Quickstart lost its Python fence');
for (const marker of ['CreateSession', 'StartExecution', 'WatchEvents']) {
  assert(quickstart.includes(marker), `Quickstart lost "${marker}"`);
}
for (const [term, target] of [
  ['Session', `${toPageUrl('/concepts/profile-session')}#session`],
  ['Execution', `${toPageUrl('/concepts/runtime-model')}#execution`],
  ['Pipeline', `${toPageUrl('/concepts/capability-tool-pipeline')}#pipeline`],
]) {
  assert(
    quickstart.includes(`[${term}](${target})`),
    `Quickstart lost the ${term} concept link`,
  );
}

const removedPublicPatterns = [
  [/\/v1\/sessions\b/, 'removed REST session path'],
  [/\/pipeline-runs\b/, 'removed Pipeline Run path'],
  [/\/stream\b/, 'removed REST stream path'],
  [/\bjob\.[a-z_]+/i, 'removed AgentK job event'],
  [/\bpex_[a-z0-9_]*/i, 'removed Pipeline Run ID'],
  [/ToolModeBackground|ToolMode\.BACKGROUND/, 'removed background Tool mode'],
  [/TEMPORAL_ADDRESS/, 'manual Temporal address'],
  [/TEMPORAL_NAMESPACE/, 'manual Temporal namespace'],
  [/AGENTK_BASE_URL/, 'manual AgentK base URL'],
  [/agentk\.client\.http/, 'undocumented Python HTTP transport'],
  [/AgentKHttpClient/, 'undocumented Java HTTP transport'],
  [/go\/client\/http/, 'undocumented Go HTTP transport'],
  [/\/sdk\/protocol\b/, 'removed SDK Protocol page'],
  [
    /kling-agent\/agent-sys\/agentk-sdk\.git/,
    'obsolete AgentK SDK module path',
  ],
];
for (const sourceFile of sourceDocs) {
  const source = await readNonEmpty(sourceFile);
  for (const [pattern, label] of removedPublicPatterns) {
    assert(
      !pattern.test(source),
      `${path.relative(rootDir, sourceFile)} contains ${label}`,
    );
  }
}

for (const markdownUrl of uniqueDocLinks) {
  const markdown = await readNonEmpty(outputPathForUrl(markdownUrl));
  for (const [pattern, label] of removedPublicPatterns) {
    assert(!pattern.test(markdown), `${markdownUrl} contains ${label}`);
  }
}

const sdkContractChecks = [
  [
    'client/sdk/go.md',
    ['go/client/grpc', 'InvokeCapability', 'StartExecution', 'WatchEvents'],
  ],
  [
    'client/sdk/python.md',
    [
      'ks-agentk-client[grpc]==0.3.2',
      'invoke_capability',
      'start_execution',
      'watch_events',
    ],
  ],
  [
    'client/sdk/java.md',
    ['agentk-client-grpc', 'invokeCapability', 'startExecution', 'watchEvents'],
  ],
  [
    'client/guides/invoke-capability.md',
    ['InvokeCapability', 'invoke_capability', 'invokeCapability'],
  ],
  ['worker/sdk/go.md', ['agentktemporal.Dial', 'PublishWorkflow', 'capability.Invoke']],
  [
    'worker/sdk/python.md',
    ['connect_temporal', 'publish_workflow', 'invoke_capability'],
  ],
  [
    'examples/go-storyboard-job-api.md',
    [
      'agentk-sdk.git/go/temporal',
      'agentk-sdk.git/go/worker/capability',
      'agentk-sdk.git/go/client/grpc',
    ],
  ],
  [
    'examples/python-storyboard-mcp.md',
    ['from agentk.worker import', 'from agentk.api.v1 import', 'from agentk.client.grpc import'],
  ],
];
for (const [relativePath, markers] of sdkContractChecks) {
  const content = await readNonEmpty(path.join(buildDir, relativePath));
  for (const marker of markers) {
    assert(content.includes(marker), `${relativePath} lost "${marker}"`);
  }
}

const diagramChecks = [
  ['index.md', ['SESSION', 'TEMPORAL', 'CAPABILITY']],
  [
    'concepts/runtime-model.md',
    ['SESSION', 'TEMPORAL', 'CAPABILITY'],
  ],
  [
    'concepts/capability-tool-pipeline.md',
    ['SESSION', 'TEMPORAL', 'CAPABILITY', 'DIRECT INVOKE'],
  ],
];
for (const [relativePath, markers] of diagramChecks) {
  const content = (
    await readNonEmpty(path.join(buildDir, relativePath))
  ).toUpperCase();
  for (const marker of markers) {
    assert(content.includes(marker), `${relativePath} lost "${marker}"`);
  }
}

const llmsTitles = linkTitles(llms);
const fullTitles = [...llmsFull.matchAll(/^## (.+)\nSource: /gm)].map(
  (match) => match[1],
);
assert(
  JSON.stringify(llmsTitles) === JSON.stringify(fullTitles),
  'llms-full.txt page order differs from llms.txt',
);

console.log(`Verified ${uniqueDocLinks.length} AI-friendly documentation pages.`);
