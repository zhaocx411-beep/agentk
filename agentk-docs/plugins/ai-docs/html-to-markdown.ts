import * as cheerio from 'cheerio';
import TurndownService from 'turndown';
import {gfm} from 'turndown-plugin-gfm';

import {stripBaseUrl} from './paths';

export type MarkdownPage = {
  routePath: string;
  markdownPath: string;
  title: string;
  description: string;
  markdown: string;
  htmlFile: string;
  headings: RenderedHeading[];
};

export type RenderedHeading = {
  level: number;
  text: string;
  markdown: string;
  anchor: string | null;
};

type ExtractPageOptions = {
  html: string;
  htmlFile: string;
  routePath: string;
  markdownPath: string;
  pageUrls: ReadonlyMap<string, string>;
  baseUrl: string;
};

const ZERO_WIDTH = /[\u200B-\u200D\uFEFF]/g;
const INTERNAL_ORIGIN = 'https://agentk.invalid';

function normalizeRoutePath(pathname: string): string {
  if (pathname === '/') {
    return pathname;
  }
  return pathname.replace(/\/+$/, '');
}

function rewriteInternalLinks(
  $: cheerio.CheerioAPI,
  article: ReturnType<cheerio.CheerioAPI>,
  pageUrls: ReadonlyMap<string, string>,
  baseUrl: string,
) {
  article.find('a[href]').each((_, element) => {
    const link = $(element);
    const href = link.attr('href');
    if (!href || href.startsWith('#')) {
      return;
    }

    let parsed: URL;
    try {
      parsed = new URL(href, INTERNAL_ORIGIN);
    } catch {
      return;
    }
    if (parsed.origin !== INTERNAL_ORIGIN) {
      return;
    }

    const logicalPath =
      stripBaseUrl(parsed.pathname, baseUrl) ?? parsed.pathname;
    const pageUrl = pageUrls.get(normalizeRoutePath(logicalPath));
    if (pageUrl) {
      link.attr('href', `${pageUrl}${parsed.hash}`);
    }
  });
}

function directLabelLines(element: Element): string[] {
  const lines: string[] = [];
  const blocks = [
    element,
    ...Array.from(
      element.querySelectorAll('div, section, article, header, li'),
    ),
  ];

  for (const block of blocks) {
    const parts = Array.from(block.children)
      .filter((child) =>
        ['CODE', 'STRONG', 'SPAN', 'B'].includes(child.tagName),
      )
      .map((child) => child.textContent.replace(ZERO_WIDTH, '').trim())
      .filter(Boolean);
    if (parts.length === 0) {
      continue;
    }
    const line = parts.join(' · ');
    if (lines.at(-1) !== line) {
      lines.push(line);
    }
  }

  if (lines.length === 0) {
    const fallback = element.textContent
      .replace(ZERO_WIDTH, '')
      .replace(/\s+/g, ' ')
      .trim();
    if (fallback) {
      lines.push(fallback);
    }
  }
  return lines;
}

function createTurndown(): TurndownService {
  const service = new TurndownService({
    bulletListMarker: '-',
    codeBlockStyle: 'fenced',
    emDelimiter: '*',
    headingStyle: 'atx',
  });
  service.use(gfm);

  service.addRule('docusaurusCodeBlock', {
    filter: (node) => node.nodeName === 'PRE',
    replacement: (_content, node) => {
      const pre = node as HTMLElement;
      const code = pre.querySelector('code');
      if (!code) {
        return '';
      }
      const languageClass = [
        ...Array.from(pre.classList),
        ...Array.from(code.classList),
      ].find((name) => name.startsWith('language-'));
      const language = languageClass?.slice('language-'.length) ?? '';
      const tokenLines = Array.from(code.querySelectorAll('.token-line'));
      const raw = (
        tokenLines.length > 0
          ? tokenLines.map((line) => line.textContent ?? '').join('\n')
          : code.textContent ?? ''
      ).replace(/\n+$/, '');
      return `\n\n\`\`\`${language}\n${raw}\n\`\`\`\n\n`;
    },
  });

  service.addRule('docusaurusAdmonition', {
    filter: (node) =>
      node.nodeName === 'DIV' &&
      (node as HTMLElement).classList.contains('theme-admonition'),
    replacement: (_content, node) => {
      const element = node as HTMLElement;
      const type =
        Array.from(element.classList)
          .find((name) => name.startsWith('theme-admonition-'))
          ?.slice('theme-admonition-'.length) ?? 'note';
      const heading = element
        .querySelector('[class*="admonitionHeading"]')
        ?.textContent?.replace(ZERO_WIDTH, '')
        .trim();
      const body = element.querySelector('[class*="admonitionContent"]');
      const bodyMarkdown = body
        ? service.turndown(body.innerHTML).trim()
        : '';
      const label = heading || type;
      const quotedBody = bodyMarkdown
        .split('\n')
        .map((line) => (line ? `> ${line}` : '>'))
        .join('\n');
      return `\n\n> **${label}**\n>${quotedBody ? `\n${quotedBody}` : ''}\n\n`;
    },
  });

  service.addRule('agentkDiagram', {
    filter: (node) => node.nodeName === 'FIGURE',
    replacement: (_content, node) => {
      const figure = node as HTMLElement;
      const title =
        figure.querySelector('figcaption strong')?.textContent?.trim() ??
        '结构图';
      const path = figure.querySelector('figcaption code')?.textContent?.trim();
      const lines = directLabelLines(figure);
      const body = lines.map((line) => `- ${line}`).join('\n');
      return [
        '',
        `> **图：${title}**`,
        ...(path ? [`> ${path}`] : []),
        '',
        body,
        '',
      ].join('\n');
    },
  });

  return service;
}

function cleanMarkdown(markdown: string): string {
  return markdown
    .replace(ZERO_WIDTH, '')
    .replace(/[ \t]+\n/g, '\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
}

export function extractMarkdownPage({
  html,
  htmlFile,
  routePath,
  markdownPath,
  pageUrls,
  baseUrl,
}: ExtractPageOptions): MarkdownPage {
  const $ = cheerio.load(html);
  const article = $('.theme-doc-markdown').first();
  if (article.length === 0) {
    throw new Error(
      `AI docs export: missing .theme-doc-markdown in ${htmlFile}`,
    );
  }

  article
    .find(
      [
        'a.hash-link',
        'button',
        'svg',
        '.theme-edit-this-page',
        '.table-of-contents',
        '[data-ai-docs-ignore]',
      ].join(','),
    )
    .remove();
  article.find('span + span').each((_, element) => {
    if ($(element).closest('pre, figure').length === 0) {
      $(element).before(' ');
    }
  });
  rewriteInternalLinks($, article, pageUrls, baseUrl);

  const title =
    article.find('h1').first().text().replace(ZERO_WIDTH, '').trim() ||
    $('meta[property="og:title"]').attr('content')?.trim() ||
    $('title').text().split('|')[0]?.trim() ||
    routePath;
  const description =
    $('meta[name="description"]').attr('content')?.trim() ?? '';

  const turndown = createTurndown();
  const headings = article
    .find('h1, h2, h3, h4, h5, h6')
    .toArray()
    .map((element): RenderedHeading => {
      const heading = $(element);
      const anchor =
        heading.attr('id') ?? heading.parents('article[id]').first().attr('id');
      return {
        level: Number(element.tagName.slice(1)),
        text: heading
          .text()
          .replace(ZERO_WIDTH, '')
          .replace(/\s+/g, ' ')
          .trim(),
        markdown: turndown.turndown(heading.html() ?? '').trim(),
        anchor: anchor || null,
      };
    });
  const markdown = cleanMarkdown(turndown.turndown(article.html() ?? ''));
  const withTitle = /^#\s+/m.test(markdown)
    ? markdown
    : `# ${title}\n\n${markdown}`;

  return {
    routePath,
    markdownPath,
    title,
    description,
    markdown: `<!-- Generated from ${routePath}; do not edit. -->\n\n${withTitle}\n`,
    htmlFile,
    headings,
  };
}
