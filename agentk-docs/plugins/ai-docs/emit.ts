import fs from 'node:fs/promises';
import path from 'node:path';
import * as cheerio from 'cheerio';

import type {MarkdownPage} from './html-to-markdown';
import {stripBaseUrl, withBaseUrl} from './paths';

export type AiDocsOptions = {
  summary: string;
  guidance: string[];
};

export type PageGroup = {
  title: string;
  pages: MarkdownPage[];
};

function normalizeRoutePath(pathname: string): string {
  if (pathname === '/') {
    return pathname;
  }
  return pathname.replace(/\/+$/, '');
}

function pageDescription(page: MarkdownPage): string {
  const description = page.description
    .replace(/\s+/g, ' ')
    .replace(/^AgentK\s*[-—|:：]\s*/i, '')
    .trim();
  return description || page.title;
}

export async function emitPageMarkdown(
  outDir: string,
  page: MarkdownPage,
): Promise<void> {
  const outputFile = path.join(outDir, page.markdownPath.replace(/^\//, ''));
  await fs.mkdir(path.dirname(outputFile), {recursive: true});
  await fs.writeFile(outputFile, page.markdown, 'utf8');
}

export async function injectMarkdownAlternate(
  page: MarkdownPage,
  baseUrl: string,
): Promise<void> {
  const html = await fs.readFile(page.htmlFile, 'utf8');
  if (html.includes('type="text/markdown"')) {
    return;
  }

  const href = withBaseUrl(page.markdownPath, baseUrl);
  const alternate = `<link rel="alternate" type="text/markdown" href="${href}">`;
  const updated = html.replace('</head>', `${alternate}</head>`);
  if (updated === html) {
    throw new Error(
      `AI docs export: unable to inject alternate link into ${page.htmlFile}`,
    );
  }
  await fs.writeFile(page.htmlFile, updated, 'utf8');
}

export function orderFromRenderedSidebar(
  htmlDocuments: string[],
  pages: MarkdownPage[],
  baseUrl: string,
): PageGroup[] {
  const pageByRoute = new Map(
    pages.map((page) => [normalizeRoutePath(page.routePath), page]),
  );

  const groups: PageGroup[] = [];
  const groupByTitle = new Map<string, PageGroup>();
  const groupByRoute = new Map<string, PageGroup>();
  const included = new Set<string>();

  for (const html of htmlDocuments) {
    const $ = cheerio.load(html);
    const menu = $('.theme-doc-sidebar-menu').first();
    if (menu.length === 0) {
      continue;
    }

    menu.find('a.menu__link[href]').each((_, link) => {
      const href = $(link).attr('href');
      if (!href) {
        return;
      }
      let pathname: string;
      try {
        pathname = new URL(href, 'https://agentk.invalid').pathname;
      } catch {
        return;
      }
      const logicalPath = stripBaseUrl(pathname, baseUrl);
      const page =
        logicalPath === undefined
          ? undefined
          : pageByRoute.get(normalizeRoutePath(logicalPath));
      if (!page || included.has(page.routePath)) {
        return;
      }

      const category = $(link)
        .parents('li.theme-doc-sidebar-item-category')
        .first();
      const categoryLabel = category
        .children('.menu__list-item-collapsible')
        .first()
        .text()
        .trim();
      const title = categoryLabel || '开始';
      let group = groupByTitle.get(title);
      if (!group) {
        group = {title, pages: []};
        groupByTitle.set(title, group);
        groups.push(group);
      }
      included.add(page.routePath);
      groupByRoute.set(page.routePath, group);
      group.pages.push(page);
    });
  }

  if (groups.length === 0) {
    throw new Error('AI docs export: rendered docs sidebar was not found');
  }

  const otherPages: MarkdownPage[] = [];
  const includedRoutes = [...groupByRoute.keys()];
  for (const page of pages.filter((candidate) => !included.has(candidate.routePath))) {
    const parentRoute = includedRoutes
      .filter((route) => page.routePath.startsWith(`${route}/`))
      .sort((left, right) => right.length - left.length)[0];
    const parentGroup = parentRoute ? groupByRoute.get(parentRoute) : undefined;
    if (parentGroup) {
      parentGroup.pages.push(page);
    } else {
      otherPages.push(page);
    }
  }
  if (otherPages.length > 0) {
    groups.push({title: '其他', pages: otherPages});
  }
  return groups;
}

export async function emitLlmsTxt(
  outDir: string,
  groups: PageGroup[],
  options: AiDocsOptions,
  baseUrl: string,
): Promise<void> {
  const sections = groups.map((group) => {
    const links = group.pages.map(
      (page) =>
        `- [${page.title}](${withBaseUrl(page.markdownPath, baseUrl)}): ${pageDescription(page)}`,
    );
    return [`## ${group.title}`, ...links].join('\n');
  });
  const output = [
    '# AgentK',
    `> ${options.summary}`,
    '',
    ...options.guidance,
    '',
    ...sections.flatMap((section) => [section, '']),
    '## Optional',
    `- [完整文档语料](${withBaseUrl('/llms-full.txt', baseUrl)}): 一次加载全部当前文档`,
    '',
  ].join('\n');
  await fs.writeFile(path.join(outDir, 'llms.txt'), output, 'utf8');
}

function withoutFirstHeading(markdown: string): string {
  return markdown
    .replace(/^<!--[\s\S]*?-->\s*/u, '')
    .replace(/^#\s+.*(?:\n+|$)/mu, '')
    .trim();
}

export async function emitLlmsFull(
  outDir: string,
  groups: PageGroup[],
  baseUrl: string,
): Promise<void> {
  const pages = groups.flatMap((group) => group.pages);
  const sections = pages.map((page) =>
    [
      `## ${page.title}`,
      `Source: ${withBaseUrl(page.routePath, baseUrl)}`,
      '',
      withoutFirstHeading(page.markdown),
    ].join('\n'),
  );
  const output = [
    '# AgentK 完整文档',
    '',
    '以下内容由当前站点构建自动生成，页面顺序与文档导航一致。',
    '',
    ...sections.flatMap((section) => [section, '', '---', '']),
  ].join('\n');
  await fs.writeFile(path.join(outDir, 'llms-full.txt'), output, 'utf8');
}
