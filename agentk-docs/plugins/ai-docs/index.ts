import fs from 'node:fs/promises';
import path from 'node:path';
import type {
  LoadContext,
  Plugin,
  PluginIdentifier,
  PluginRouteConfig,
  RouteConfig,
  RouteMetadata,
} from '@docusaurus/types';

import {
  emitLlmsFull,
  emitLlmsTxt,
  emitPageMarkdown,
  injectMarkdownAlternate,
  orderFromRenderedSidebar,
  type AiDocsOptions,
} from './emit';
import {extractMarkdownPage, type MarkdownPage} from './html-to-markdown';
import {stripBaseUrl, toPageUrl} from './paths';
import {emitSearchDocuments} from './search-documents';

type FlattenedRoute = {
  path: string;
  metadata?: RouteMetadata;
  plugin?: PluginIdentifier;
};

function isPluginIdentifier(value: unknown): value is PluginIdentifier {
  return (
    typeof value === 'object' &&
    value !== null &&
    'name' in value &&
    typeof value.name === 'string' &&
    'id' in value &&
    typeof value.id === 'string'
  );
}

function flattenRoutes(
  routes: Array<PluginRouteConfig | RouteConfig>,
  inheritedPlugin?: PluginIdentifier,
): FlattenedRoute[] {
  return routes.flatMap((route) => {
    const plugin = isPluginIdentifier(route.plugin)
      ? route.plugin
      : inheritedPlugin;
    const current =
      typeof route.path === 'string'
        ? [{path: route.path, metadata: route.metadata, plugin}]
        : [];
    return [
      ...current,
      ...flattenRoutes(route.routes ?? [], plugin),
    ];
  });
}

function normalizeRoutePath(routePath: string, baseUrl: string): string {
  const withoutBase = stripBaseUrl(routePath, baseUrl);
  if (withoutBase === undefined) {
    throw new Error(
      `AI docs export: route ${routePath} is outside base URL ${baseUrl}`,
    );
  }
  if (withoutBase === '/') {
    return withoutBase;
  }
  return withoutBase.replace(/\/+$/, '');
}

function routeToMarkdownPath(routePath: string): string {
  return routePath === '/' ? '/index.md' : `${routePath}.md`;
}

async function resolveHtmlFile(
  outDir: string,
  routePath: string,
): Promise<string> {
  const relative = routePath.replace(/^\/|\/$/g, '');
  const candidates =
    relative.length === 0
      ? [path.join(outDir, 'index.html')]
      : [
          path.join(outDir, relative, 'index.html'),
          path.join(outDir, `${relative}.html`),
        ];
  for (const candidate of candidates) {
    try {
      await fs.access(candidate);
      return candidate;
    } catch {
      // Try the next Docusaurus output shape.
    }
  }
  throw new Error(
    `AI docs export: no rendered HTML found for route ${routePath}`,
  );
}

export default function aiDocsPlugin(
  context: LoadContext,
  options: AiDocsOptions,
): Plugin<void> {
  return {
    name: 'agentk-ai-docs',
    async postBuild({
      routes,
      routesBuildMetadata,
      outDir,
      baseUrl,
    }) {
      const routeRecords = flattenRoutes(routes)
        .filter(
          (route) =>
            route.plugin?.name === 'docusaurus-plugin-content-docs' &&
            typeof route.metadata?.sourceFilePath === 'string' &&
            !routesBuildMetadata[route.path]?.noIndex,
        )
        .map((route) => ({
          ...route,
          path: normalizeRoutePath(route.path, baseUrl),
        }));
      const uniqueRoutes = [
        ...new Map(routeRecords.map((route) => [route.path, route])).values(),
      ];
      if (uniqueRoutes.length === 0) {
        throw new Error('AI docs export: no documentation routes were found');
      }

      const siteUrl = new URL(baseUrl, context.siteConfig.url);
      const pageUrls = new Map(
        uniqueRoutes.map((route) => [route.path, toPageUrl(route.path, siteUrl)]),
      );
      const pages: MarkdownPage[] = [];
      for (const route of uniqueRoutes) {
        const htmlFile = await resolveHtmlFile(outDir, route.path);
        const html = await fs.readFile(htmlFile, 'utf8');
        const page = extractMarkdownPage({
          html,
          htmlFile,
          routePath: route.path,
          markdownPath: routeToMarkdownPath(route.path),
          pageUrls,
          baseUrl,
        });
        pages.push(page);
        await emitPageMarkdown(outDir, page);
        await injectMarkdownAlternate(page, baseUrl);
      }

      const sidebarHtmlDocuments = await Promise.all(
        pages.map((page) => fs.readFile(page.htmlFile, 'utf8')),
      );
      const groups = orderFromRenderedSidebar(
        sidebarHtmlDocuments,
        pages,
        baseUrl,
      );
      await emitLlmsTxt(outDir, groups, options, baseUrl);
      await emitLlmsFull(outDir, groups, baseUrl);
      await emitSearchDocuments(context.siteDir, groups);
    },
  };
}
