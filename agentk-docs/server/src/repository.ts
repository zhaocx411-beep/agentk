import fs from 'node:fs/promises';
import path from 'node:path';

import {
  loadSearchDocuments,
  type SearchDocuments,
  type SearchPage,
} from './documents.js';
import { search, type SearchRequest } from './search.js';

const MAX_DOCUMENT_BYTES = 80 * 1024;

type PageEntry = {
  page: SearchPage;
  diskPath: string;
  url: string;
};

export type DocsSearchHit = {
  url: string;
  title: string;
  headingPath: string[];
  excerpt: string;
};

export type ReadDocument = {
  url: string;
  title: string;
  markdown: string;
};

export class DocsRepositoryError extends Error {
  constructor(message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = 'DocsRepositoryError';
  }
}

function repositoryError(message: string, cause?: unknown): DocsRepositoryError {
  return new DocsRepositoryError(message, cause === undefined ? undefined : { cause });
}

function validateMarkdownPath(markdownPath: string): void {
  if (
    !markdownPath.startsWith('/') ||
    !markdownPath.endsWith('.md') ||
    markdownPath.includes('\\') ||
    markdownPath.includes('\0') ||
    path.posix.normalize(markdownPath) !== markdownPath
  ) {
    throw repositoryError(`Invalid indexed Markdown path: ${markdownPath}`);
  }
}

function assertInside(root: string, candidate: string, label: string): void {
  const relative = path.relative(root, candidate);
  if (relative === '' || (!relative.startsWith('..') && !path.isAbsolute(relative))) {
    return;
  }
  throw repositoryError(`${label} resolves outside the documentation build directory`);
}

export function toPageUrl(route: string, siteUrl: URL): string {
  return new URL(route === '/' ? '' : route.replace(/^\//, ''), siteUrl).href;
}

export class DocsRepository {
  readonly siteUrl: URL;

  readonly #documents: SearchDocuments;
  readonly #byUrl: ReadonlyMap<string, PageEntry>;
  readonly #byRoute: ReadonlyMap<string, PageEntry>;

  constructor(
    documents: SearchDocuments,
    entries: readonly PageEntry[],
    siteUrl: URL,
  ) {
    this.siteUrl = siteUrl;
    this.#documents = documents;
    this.#byUrl = new Map(entries.map((entry) => [entry.url, entry]));
    this.#byRoute = new Map(entries.map((entry) => [entry.page.route, entry]));
  }

  get urls(): string[] {
    return [...this.#byUrl.keys()];
  }

  search(request: SearchRequest): DocsSearchHit[] {
    return search(this.#documents, request).map((hit) => {
      const entry = this.#byRoute.get(hit.route);
      if (entry === undefined) {
        throw repositoryError(`Indexed route has no page: ${hit.route}`);
      }
      return {
        url: hit.anchor === null ? entry.url : `${entry.url}#${hit.anchor}`,
        title: hit.title,
        headingPath: hit.headingPath,
        excerpt: hit.excerpt,
      };
    });
  }

  async read(url: string): Promise<ReadDocument> {
    const entry = this.#byUrl.get(this.#canonical(url));
    if (entry === undefined) {
      throw repositoryError(`Unknown AgentK documentation page: ${url}`);
    }
    let markdown: string;
    try {
      markdown = await fs.readFile(entry.diskPath, 'utf8');
    } catch (cause) {
      throw repositoryError(
        `Unable to read AgentK documentation page: ${url}`,
        cause,
      );
    }
    return { url: entry.url, title: entry.page.title, markdown };
  }

  #canonical(value: string): string {
    let url: URL;
    try {
      url = new URL(value);
    } catch (cause) {
      throw repositoryError(
        `Not an absolute AgentK documentation URL: ${value}`,
        cause,
      );
    }
    if (url.origin !== this.siteUrl.origin || url.search !== '') {
      throw repositoryError(`Not an AgentK documentation URL: ${value}`);
    }
    url.hash = '';
    if (`${url.pathname}/` === this.siteUrl.pathname) {
      url.pathname = this.siteUrl.pathname;
    } else if (url.pathname !== this.siteUrl.pathname) {
      url.pathname = url.pathname.replace(/\/+$/, '');
    }
    return url.href;
  }
}

export async function openDocsRepository(
  root: string,
  siteUrl: URL,
): Promise<DocsRepository> {
  const corpusPath = path.join(root, 'generated', 'search-documents.json');
  let documents: SearchDocuments;
  try {
    documents = await loadSearchDocuments(corpusPath);
  } catch (cause) {
    throw repositoryError(
      'Unable to load the AgentK documentation search corpus. Run pnpm build:site first.',
      cause,
    );
  }

  const buildRoot = path.join(root, 'build');
  let realBuildRoot: string;
  try {
    realBuildRoot = await fs.realpath(buildRoot);
  } catch (cause) {
    throw repositoryError(
      'Unable to open the AgentK documentation build. Run pnpm build:site first.',
      cause,
    );
  }

  const urls = new Set<string>();
  const entries: PageEntry[] = [];
  for (const page of documents.pages) {
    validateMarkdownPath(page.markdownPath);
    const url = toPageUrl(page.route, siteUrl);
    if (urls.has(url)) {
      throw repositoryError(`Duplicate indexed documentation page: ${url}`);
    }
    urls.add(url);

    const candidate = path.resolve(buildRoot, page.markdownPath.slice(1));
    assertInside(buildRoot, candidate, page.markdownPath);
    let diskPath: string;
    try {
      diskPath = await fs.realpath(candidate);
    } catch (cause) {
      throw repositoryError(
        `Indexed AgentK documentation page is missing: ${page.markdownPath}. Run pnpm build:site first.`,
        cause,
      );
    }
    assertInside(realBuildRoot, diskPath, page.markdownPath);
    let isFile: boolean;
    let size: number;
    try {
      const stats = await fs.stat(diskPath);
      isFile = stats.isFile();
      size = stats.size;
    } catch (cause) {
      throw repositoryError(
        `Unable to inspect indexed AgentK documentation page: ${page.markdownPath}`,
        cause,
      );
    }
    if (!isFile) {
      throw repositoryError(
        `Indexed AgentK documentation page is not a file: ${page.markdownPath}`,
      );
    }
    // Checked while indexing so an oversized page fails startup instead of
    // surfacing as an unreadable search hit.
    if (size > MAX_DOCUMENT_BYTES) {
      throw repositoryError(
        `Indexed AgentK documentation page exceeds the ${
          MAX_DOCUMENT_BYTES / 1024
        } KiB read limit: ${page.markdownPath}`,
      );
    }
    entries.push({ page, diskPath, url });
  }

  return new DocsRepository(documents, entries, siteUrl);
}

export { MAX_DOCUMENT_BYTES };
