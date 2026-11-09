import type {
  SearchArea,
  SearchDocuments,
  SearchKind,
  SearchLanguage,
  SearchSection,
} from './documents.js';
import {normalizeText, tokenize} from './tokenize.js';

export const EXCERPT_MAX_CHARACTERS = 600;

const FENCE_MARKER = /^ {0,3}(`{3,}|~{3,})/u;
const ELLIPSIS = '…';

export type SearchRequest = {
  query: string;
  area?: SearchArea;
  kind?: SearchKind;
  language?: SearchLanguage;
  limit?: number;
};

export type SearchHit = {
  route: string;
  anchor: string | null;
  title: string;
  headingPath: string[];
  excerpt: string;
  score: number;
};

type RankedHit = SearchHit & {
  pageIndex: number;
  sectionIndex: number;
  sectionId: string;
};

type FenceBlock = {
  start: number;
  end: number;
  opening: string;
  marker: string;
};

function includes(haystack: string, needle: string): boolean {
  return needle.length > 0 && normalizeText(haystack).includes(needle);
}

function scoreSection(
  title: string,
  section: SearchSection,
  query: string,
  tokens: string[],
): number {
  const heading = section.headingPath.join(' ');
  const normalizedTerms = section.codeTerms.map(normalizeText);
  let score = 0;
  if (normalizedTerms.includes(query)) {
    score += 50;
  }
  if (includes(title, query)) {
    score += 30;
  }
  if (includes(heading, query)) {
    score += 20;
  }
  if (includes(section.markdown, query)) {
    score += 8;
  }
  for (const token of tokens) {
    if (includes(title, token)) {
      score += 12;
    }
    if (normalizedTerms.some((term) => term === token)) {
      score += 10;
    }
    if (includes(heading, token)) {
      score += 8;
    }
    if (includes(section.markdown, token)) {
      score += 1;
    }
  }
  return score;
}

function bestMarkdownMatch(
  section: SearchSection,
  query: string,
  tokens: string[],
): number {
  const normalizedMarkdown = normalizeText(section.markdown);
  const normalizedTerms = new Set(section.codeTerms.map(normalizeText));
  const candidates = [
    {needle: query, weight: normalizedTerms.has(query) ? 50 : 8},
    ...tokens.map((token) => ({
      needle: token,
      weight: normalizedTerms.has(token) ? 10 : 1,
    })),
  ]
    .map((candidate) => ({
      ...candidate,
      index: normalizedMarkdown.indexOf(candidate.needle),
    }))
    .filter((candidate) => candidate.index >= 0)
    .sort(
      (left, right) => right.weight - left.weight || left.index - right.index,
    );
  return candidates[0]?.index ?? 0;
}

function fenceBlocks(lines: readonly string[]): FenceBlock[] {
  const blocks: FenceBlock[] = [];
  let open: {start: number; marker: string} | null = null;
  for (const [index, line] of lines.entries()) {
    const marker = line.match(FENCE_MARKER)?.[1];
    if (marker === undefined) {
      continue;
    }
    if (open === null) {
      open = {start: index, marker};
      continue;
    }
    if (
      marker[0] === open.marker[0] &&
      marker.length >= open.marker.length &&
      line.trim() === marker
    ) {
      blocks.push({
        start: open.start,
        end: index,
        opening: lines[open.start] ?? open.marker,
        marker: open.marker,
      });
      open = null;
    }
  }
  if (open !== null) {
    blocks.push({
      start: open.start,
      end: lines.length - 1,
      opening: lines[open.start] ?? open.marker,
      marker: open.marker,
    });
  }
  return blocks;
}

function lineOf(lines: readonly string[], index: number): number {
  let offset = 0;
  for (const [line, value] of lines.entries()) {
    offset += value.length + 1;
    if (index < offset) {
      return line;
    }
  }
  return Math.max(0, lines.length - 1);
}

function startOfLine(lines: readonly string[], line: number): number {
  let offset = 0;
  for (let index = 0; index < line; index += 1) {
    offset += (lines[index]?.length ?? 0) + 1;
  }
  return offset;
}

function withoutPartialLinks(value: string): string {
  return value
    .replace(/^[^[(]*\)/u, '')
    .replace(/\[[^\]]*\]\([^)]*$/u, '')
    .replace(/\[[^\]]*$/u, '');
}

function lineWindow(line: string, offset: number, budget: number): string {
  const room = Math.max(1, budget - 2 * ELLIPSIS.length);
  if (line.length <= room) {
    return line;
  }
  const start = Math.max(
    0,
    Math.min(offset - Math.floor(room / 3), line.length - room),
  );
  const end = Math.min(line.length, start + room);
  const prefix = start > 0 ? ELLIPSIS : '';
  const suffix = end < line.length ? ELLIPSIS : '';
  return `${prefix}${withoutPartialLinks(line.slice(start, end))}${suffix}`;
}

function spanLength(
  lines: readonly string[],
  start: number,
  end: number,
): number {
  let length = -1;
  for (let index = start; index <= end; index += 1) {
    length += (lines[index]?.length ?? 0) + 1;
  }
  return Math.max(0, length);
}

function grow(
  lines: readonly string[],
  center: number,
  from: number,
  to: number,
  budget: number,
): {start: number; end: number} {
  let start = from;
  let end = to;
  let size = spanLength(lines, start, end);
  for (;;) {
    const forward =
      end + 1 < lines.length ? (lines[end + 1]?.length ?? 0) + 1 : -1;
    if (forward >= 0 && size + forward <= budget) {
      end += 1;
      size += forward;
      continue;
    }
    const backward = start > 0 ? (lines[start - 1]?.length ?? 0) + 1 : -1;
    if (backward >= 0 && size + backward <= budget) {
      start -= 1;
      size += backward;
      continue;
    }
    return {start: Math.min(start, center), end: Math.max(end, center)};
  }
}

function dropPartialBlocks(
  lines: readonly string[],
  blocks: readonly FenceBlock[],
  center: number,
  from: number,
  to: number,
): string {
  let start = from;
  let end = to;
  for (const block of blocks) {
    if (block.start < start && block.end >= start) {
      start = block.end + 1;
    }
    if (block.start <= end && block.end > end) {
      end = block.start - 1;
    }
  }
  if (start > center || end < center) {
    start = center;
    end = center;
  }
  const prefix = start > 0 ? `${ELLIPSIS}\n` : '';
  const suffix = end < lines.length - 1 ? `\n${ELLIPSIS}` : '';
  return `${prefix}${lines.slice(start, end + 1).join('\n')}${suffix}`;
}

function excerptAround(markdown: string, index: number): string {
  if (markdown.length <= EXCERPT_MAX_CHARACTERS) {
    return markdown;
  }
  const lines = markdown.split('\n');
  const blocks = fenceBlocks(lines);
  const center = lineOf(lines, index);
  const line = lines[center] ?? '';
  const offset = index - startOfLine(lines, center);
  const decorated = EXCERPT_MAX_CHARACTERS - 2 * (ELLIPSIS.length + 1);
  const block = blocks.find(
    (candidate) => center >= candidate.start && center <= candidate.end,
  );

  if (block === undefined) {
    if (line.length > decorated) {
      return lineWindow(line, offset, EXCERPT_MAX_CHARACTERS);
    }
    const span = grow(lines, center, center, center, decorated);
    return dropPartialBlocks(lines, blocks, center, span.start, span.end);
  }

  if (
    line.length <= decorated &&
    spanLength(lines, block.start, block.end) <= decorated
  ) {
    const span = grow(lines, center, block.start, block.end, decorated);
    return dropPartialBlocks(lines, blocks, center, span.start, span.end);
  }

  const budget = Math.max(
    1,
    EXCERPT_MAX_CHARACTERS - block.opening.length - block.marker.length - 2,
  );
  if (line.length > budget) {
    return [block.opening, lineWindow(line, offset, budget), block.marker].join(
      '\n',
    );
  }
  const span = grow(lines, center, center, center, budget);
  const inner = lines.slice(
    Math.max(span.start, block.start + 1),
    Math.min(span.end, Math.max(block.start + 1, block.end - 1)) + 1,
  );
  return [block.opening, ...inner, block.marker].join('\n');
}

export function search(
  documents: SearchDocuments,
  request: SearchRequest,
): SearchHit[] {
  const query = normalizeText(request.query.trim());
  if (!query) {
    throw new Error('Search query must not be empty');
  }
  const limit = request.limit ?? 8;
  if (!Number.isInteger(limit) || limit < 1 || limit > 20) {
    throw new Error('Search limit must be an integer between 1 and 20');
  }
  const tokens = tokenize(request.query);
  const hits: RankedHit[] = [];

  documents.pages.forEach((page, pageIndex) => {
    if (
      (request.area && page.area !== request.area) ||
      (request.kind && page.kind !== request.kind) ||
      (request.language && page.language !== request.language)
    ) {
      return;
    }
    page.sections.forEach((section, sectionIndex) => {
      const score = scoreSection(page.title, section, query, tokens);
      if (score === 0) {
        return;
      }
      hits.push({
        sectionId: section.id,
        route: page.route,
        anchor: section.anchor,
        title: page.title,
        headingPath: section.headingPath,
        excerpt: excerptAround(
          section.markdown,
          bestMarkdownMatch(section, query, tokens),
        ),
        score,
        pageIndex,
        sectionIndex,
      });
    });
  });

  return hits
    .sort(
      (left, right) =>
        right.score - left.score ||
        left.pageIndex - right.pageIndex ||
        left.sectionIndex - right.sectionIndex ||
        left.sectionId.localeCompare(right.sectionId),
    )
    .slice(0, limit)
    .map(
      ({
        pageIndex: _pageIndex,
        sectionIndex: _sectionIndex,
        sectionId: _sectionId,
        ...hit
      }) => hit,
    );
}
