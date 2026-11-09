import fs from 'node:fs/promises';
import path from 'node:path';

import type {PageGroup} from './emit';
import type {MarkdownPage, RenderedHeading} from './html-to-markdown';

export const SEARCH_DOCUMENTS_FORMAT = 'agentk-docs-search-v1' as const;

export type SearchArea =
  | 'intro'
  | 'concepts'
  | 'client'
  | 'worker'
  | 'reference'
  | 'examples'
  | 'capabilities'
  | 'platform';

export type SearchKind =
  | 'overview'
  | 'guide'
  | 'concept'
  | 'example'
  | 'api'
  | 'catalog'
  | 'platform';

export type SearchLanguage = 'go' | 'python' | 'java' | null;

export type SearchDocuments = {
  format: typeof SEARCH_DOCUMENTS_FORMAT;
  pages: SearchPage[];
};

export type SearchPage = {
  route: string;
  markdownPath: string;
  title: string;
  description: string;
  sidebarGroup: string;
  area: SearchArea;
  kind: SearchKind;
  language: SearchLanguage;
  sections: SearchSection[];
};

export type SearchSection = {
  id: string;
  anchor: string | null;
  headingPath: string[];
  markdown: string;
  codeTerms: string[];
};

type MarkdownHeading = {
  level: number;
  label: string;
  line: number;
};

type SectionSource = {
  anchor: string | null;
  headingPath: string[];
  markdown: string;
  ordinal: number;
};

const SECTION_MAX_CHARACTERS = 6_000;
const GENERATED_COMMENT = /^<!-- Generated from [^\n]+; do not edit\. -->\s*/u;
const IDENTIFIER =
  /[\p{L}_$][\p{L}\p{N}_$]*(?:[./_:#-][\p{L}\p{N}_$-]+)*/gu;

function cleanSearchMarkdown(markdown: string): string {
  return markdown.replace(/\[¤\]\(#[^)]+\)/gu, '').trim();
}

function cleanHeadingText(text: string): string {
  return text.replace(/¤/gu, '').replace(/\s+/gu, ' ').trim();
}

function normalizeInlineMarkdown(value: string): string {
  return value.replace(/\s+/gu, ' ').trim();
}

function parseMarkdownHeadings(markdown: string): MarkdownHeading[] {
  const headings: MarkdownHeading[] = [];
  const lines = markdown.split('\n');
  let fence: {character: string; length: number} | null = null;

  for (const [line, value] of lines.entries()) {
    const fenceMatch = value.match(/^ {0,3}(`{3,}|~{3,})/u);
    if (fenceMatch?.[1]) {
      const marker = fenceMatch[1];
      if (!fence) {
        fence = {character: marker[0] ?? '', length: marker.length};
      } else if (
        marker[0] === fence.character &&
        marker.length >= fence.length &&
        value.trim() === marker
      ) {
        fence = null;
      }
      continue;
    }
    if (fence) {
      continue;
    }

    const heading = value.match(/^(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$/u);
    if (heading?.[1] && heading[2]) {
      headings.push({
        level: heading[1].length,
        label: normalizeInlineMarkdown(heading[2]),
        line,
      });
    }
  }
  if (fence) {
    throw new Error('Search documents: unterminated fenced code block');
  }
  return headings;
}

function assertHeadingMapping(
  page: MarkdownPage,
  markdownHeadings: MarkdownHeading[],
): void {
  const markdownSections = markdownHeadings.filter(
    (heading) => heading.level >= 2,
  );
  const renderedSections = page.headings.filter(
    (heading) => heading.level >= 2,
  );
  if (markdownSections.length !== renderedSections.length) {
    throw new Error(
      `Search documents: ${page.routePath} has ${markdownSections.length} Markdown section headings but ${renderedSections.length} rendered section headings`,
    );
  }
  for (const [index, markdownHeading] of markdownSections.entries()) {
    const rendered = renderedSections[index];
    if (
      !rendered ||
      markdownHeading.level !== rendered.level ||
      markdownHeading.label !== normalizeInlineMarkdown(rendered.markdown)
    ) {
      throw new Error(
        `Search documents: heading ${index + 1} differs between Markdown and HTML for ${page.routePath}`,
      );
    }
  }
}

function routeClassification(route: string): {
  area: SearchArea;
  kind: SearchKind;
  language: SearchLanguage;
} {
  let area: SearchArea;
  let kind: SearchKind;
  if (route === '/') {
    area = 'intro';
    kind = 'overview';
  } else if (route === '/development' || route === '/mcp') {
    area = 'intro';
    kind = 'guide';
  } else if (route === '/capabilities') {
    area = 'capabilities';
    kind = 'catalog';
  } else if (route === '/client' || route.startsWith('/client/')) {
    area = 'client';
    kind = route === '/client' ? 'overview' : 'guide';
  } else if (route === '/worker' || route.startsWith('/worker/')) {
    area = 'worker';
    kind = route === '/worker' ? 'overview' : 'guide';
  } else if (route === '/reference' || route.startsWith('/reference/')) {
    area = 'reference';
    kind = route === '/reference' ? 'overview' : 'api';
  } else if (route.startsWith('/concepts/')) {
    area = 'concepts';
    kind = 'concept';
  } else if (route.startsWith('/examples/')) {
    area = 'examples';
    kind = 'example';
  } else if (route === '/platform' || route.startsWith('/platform/')) {
    area = 'platform';
    kind = 'platform';
  } else {
    throw new Error(`Search documents: no classification for route ${route}`);
  }

  const languageMatch = route.match(
    /^\/(?:reference\/(go|python|java)(?:\/|$)|(?:client|worker)\/sdk\/(go|python|java)$|examples\/(go|python|java)[-/])/u,
  );
  const language = languageMatch?.slice(1).find(Boolean) as
    | Exclude<SearchLanguage, null>
    | undefined;
  return {area, kind, language: language ?? null};
}

function markdownBlocks(markdown: string): string[] {
  const blocks: string[] = [];
  const lines = markdown.trim().split('\n');
  let current: string[] = [];
  let fence: {character: string; length: number} | null = null;

  const flush = () => {
    const block = current.join('\n').trim();
    if (block) {
      blocks.push(block);
    }
    current = [];
  };

  for (const line of lines) {
    const marker = line.match(/^ {0,3}(`{3,}|~{3,})/u)?.[1];
    if (marker) {
      if (!fence) {
        fence = {character: marker[0] ?? '', length: marker.length};
      } else if (
        marker[0] === fence.character &&
        marker.length >= fence.length &&
        line.trim() === marker
      ) {
        fence = null;
      }
    }
    if (!fence && line.trim() === '') {
      flush();
    } else {
      current.push(line);
    }
  }
  flush();
  if (fence) {
    throw new Error(
      'Search documents: unterminated fenced code block while splitting',
    );
  }
  return blocks;
}

function packLines(lines: string[], prefix = '', suffix = ''): string[] {
  const chunks: string[] = [];
  let current: string[] = [];
  for (const line of lines) {
    const candidate = `${prefix}${[...current, line].join('\n')}${suffix}`;
    if (current.length > 0 && candidate.length > SECTION_MAX_CHARACTERS) {
      chunks.push(`${prefix}${current.join('\n')}${suffix}`);
      current = [line];
    } else {
      current.push(line);
    }
  }
  if (current.length > 0) {
    chunks.push(`${prefix}${current.join('\n')}${suffix}`);
  }
  return chunks;
}

function splitOversizedBlock(block: string): string[] {
  if (block.length <= SECTION_MAX_CHARACTERS) {
    return [block];
  }
  if (/^ {0,3}(`{3,}|~{3,})/u.test(block)) {
    return [block];
  }

  const htmlTable = block.match(
    /^(<table\b[^>]*>)(<thead>[\s\S]*?<\/thead>)?<tbody>([\s\S]*?)<\/tbody><\/table>$/u,
  );
  if (htmlTable?.[1] && htmlTable[3]) {
    const rows = htmlTable[3].match(/<tr\b[^>]*>[\s\S]*?<\/tr>/gu) ?? [];
    if (rows.length > 0) {
      return packLines(
        rows,
        `${htmlTable[1]}${htmlTable[2] ?? ''}<tbody>`,
        '</tbody></table>',
      );
    }
  }

  const lines = block.split('\n');
  if (
    lines.length >= 3 &&
    /^\s*\|.*\|\s*$/u.test(lines[0] ?? '') &&
    /^\s*\|(?:\s*:?-+:?\s*\|)+\s*$/u.test(lines[1] ?? '')
  ) {
    const header = `${lines[0]}\n${lines[1]}\n`;
    return packLines(lines.slice(2), header);
  }
  if (lines.length > 1) {
    return packLines(lines);
  }

  const chunks: string[] = [];
  let remaining = block;
  while (remaining.length > SECTION_MAX_CHARACTERS) {
    const boundary = remaining.lastIndexOf(' ', SECTION_MAX_CHARACTERS);
    const end = boundary > 0 ? boundary : SECTION_MAX_CHARACTERS;
    chunks.push(remaining.slice(0, end).trim());
    remaining = remaining.slice(end).trim();
  }
  if (remaining) {
    chunks.push(remaining);
  }
  return chunks;
}

function splitSection(markdown: string): string[] {
  const blocks = markdownBlocks(markdown).flatMap(splitOversizedBlock);
  const chunks: string[] = [];
  let chunk = '';
  for (const block of blocks) {
    const candidate = chunk ? `${chunk}\n\n${block}` : block;
    if (chunk && candidate.length > SECTION_MAX_CHARACTERS) {
      chunks.push(chunk);
      chunk = block;
    } else {
      chunk = candidate;
    }
  }
  if (chunk) {
    chunks.push(chunk);
  }
  return chunks;
}

function addIdentifierTerms(terms: Set<string>, identifier: string): void {
  const cleaned = identifier.replace(/^[./_:#-]+|[./_:#-]+$/gu, '');
  if (!cleaned) {
    return;
  }
  terms.add(cleaned);
  for (const separated of cleaned.split(/[./_:#-]+/u)) {
    if (!separated) {
      continue;
    }
    terms.add(separated);
    for (const part of separated
      .replace(/([\p{Ll}\p{N}])(\p{Lu})/gu, '$1 $2')
      .replace(/(\p{Lu}+)(\p{Lu}\p{Ll})/gu, '$1 $2')
      .split(/\s+/u)) {
      if (part) {
        terms.add(part);
      }
    }
  }
}

function codeTerms(headingPath: string[], markdown: string): string[] {
  const terms = new Set<string>();
  const sources = [...headingPath];
  for (const match of markdown.matchAll(/`([^`\n]+)`/gu)) {
    if (match[1]) {
      sources.push(match[1]);
    }
  }
  let fence = false;
  for (const line of markdown.split('\n')) {
    if (/^ {0,3}(`{3,}|~{3,})/u.test(line)) {
      fence = !fence;
      continue;
    }
    if (fence) {
      sources.push(line);
    }
  }
  for (const source of sources) {
    for (const match of source.matchAll(IDENTIFIER)) {
      addIdentifierTerms(terms, match[0]);
    }
  }
  return [...terms];
}

function sectionSources(page: MarkdownPage): SectionSource[] {
  const markdown = page.markdown.replace(GENERATED_COMMENT, '').trim();
  const lines = markdown.split('\n');
  const headings = parseMarkdownHeadings(markdown);
  assertHeadingMapping(page, headings);
  const renderedByLine = new Map<number, RenderedHeading>();
  const markdownSections = headings.filter((heading) => heading.level >= 2);
  const renderedSections = page.headings.filter(
    (heading) => heading.level >= 2,
  );
  markdownSections.forEach((heading, index) => {
    const rendered = renderedSections[index];
    if (rendered) {
      renderedByLine.set(heading.line, rendered);
    }
  });

  const firstH1 = headings.find((heading) => heading.level === 1);
  if (!firstH1) {
    throw new Error(`Search documents: ${page.routePath} has no H1`);
  }
  const sectionHeadings = headings.filter((heading) => heading.level >= 2);
  const sources: SectionSource[] = [];
  const overviewEnd = sectionHeadings[0]?.line ?? lines.length;
  const overview = lines
    .slice(firstH1.line + 1, overviewEnd)
    .join('\n')
    .trim();
  if (overview) {
    sources.push({
      anchor: null,
      headingPath: [],
      markdown: overview,
      ordinal: 0,
    });
  }

  const headingStack: Array<{level: number; text: string}> = [];
  for (const [index, heading] of sectionHeadings.entries()) {
    const rendered = renderedByLine.get(heading.line);
    if (!rendered) {
      throw new Error(
        `Search documents: missing rendered heading for ${page.routePath}`,
      );
    }
    while (
      headingStack.at(-1)?.level &&
      headingStack.at(-1)!.level >= heading.level
    ) {
      headingStack.pop();
    }
    headingStack.push({
      level: heading.level,
      text: cleanHeadingText(rendered.text),
    });
    const end = sectionHeadings[index + 1]?.line ?? lines.length;
    sources.push({
      anchor: rendered.anchor,
      headingPath: headingStack.map((item) => item.text),
      markdown: lines.slice(heading.line, end).join('\n').trim(),
      ordinal: index + 1,
    });
  }
  if (sources.length === 0) {
    const body = lines
      .slice(firstH1.line + 1)
      .join('\n')
      .trim();
    sources.push({
      anchor: null,
      headingPath: [],
      markdown: body || lines[firstH1.line] || `# ${page.title}`,
      ordinal: 0,
    });
  }
  return sources;
}

function sectionsForPage(page: MarkdownPage): SearchSection[] {
  const prepared = sectionSources(page).map((source) => ({
    source,
    chunks: splitSection(cleanSearchMarkdown(source.markdown)),
    baseId: source.anchor
      ? `${page.routePath}#${source.anchor}`
      : `${page.routePath}#${source.headingPath.length === 0 ? 'overview' : `section-${source.ordinal}`}`,
  }));
  const totals = new Map<string, number>();
  for (const item of prepared) {
    totals.set(
      item.baseId,
      (totals.get(item.baseId) ?? 0) + item.chunks.length,
    );
  }
  const positions = new Map<string, number>();
  return prepared.flatMap(({source, chunks, baseId}) =>
    chunks.map((markdown) => {
      const position = (positions.get(baseId) ?? 0) + 1;
      positions.set(baseId, position);
      return {
        id: (totals.get(baseId) ?? 0) > 1 ? `${baseId}:${position}` : baseId,
        anchor: source.anchor,
        headingPath: source.headingPath,
        markdown,
        codeTerms: codeTerms([page.title, ...source.headingPath], markdown),
      };
    }),
  );
}

export function createSearchDocuments(groups: PageGroup[]): SearchDocuments {
  const pages = groups.flatMap((group) =>
    group.pages.map((page): SearchPage => {
      const classification = routeClassification(page.routePath);
      return {
        route: page.routePath,
        markdownPath: page.markdownPath,
        title: page.title,
        description: page.description,
        sidebarGroup: group.title,
        ...classification,
        sections: sectionsForPage(page),
      };
    }),
  );
  const routes = new Set<string>();
  const sectionIds = new Set<string>();
  for (const page of pages) {
    if (routes.has(page.route)) {
      throw new Error(`Search documents: duplicate route ${page.route}`);
    }
    routes.add(page.route);
    for (const section of page.sections) {
      if (sectionIds.has(section.id)) {
        throw new Error(`Search documents: duplicate section ID ${section.id}`);
      }
      sectionIds.add(section.id);
    }
  }
  return {format: SEARCH_DOCUMENTS_FORMAT, pages};
}

export async function emitSearchDocuments(
  siteDir: string,
  groups: PageGroup[],
): Promise<void> {
  const outputDir = path.join(siteDir, 'generated');
  const outputFile = path.join(outputDir, 'search-documents.json');
  await fs.rm(outputFile, {force: true});
  const documents = createSearchDocuments(groups);
  await fs.mkdir(outputDir, {recursive: true});
  await fs.writeFile(
    outputFile,
    `${JSON.stringify(documents, null, 2)}\n`,
    'utf8',
  );
}
