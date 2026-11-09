import { McpServer } from '@modelcontextprotocol/server';
import * as z from 'zod/v4';

import {
  SEARCH_AREAS,
  SEARCH_KINDS,
  SEARCH_LANGUAGES,
} from './documents.js';
import { DocsRepository, type DocsSearchHit } from './repository.js';

const SERVER_INSTRUCTIONS = `AgentK documentation. Use search_docs for concepts, guides, examples and usage questions, lookup_api for SDK types, methods, functions and fields, and read_doc to read one page in full.

Every url in a result is an absolute https URL of a documentation page. It is never a local file path, so do not try to open it from disk. Pass a url back to read_doc to read that page, and quote it unchanged in answers so the reader can open it. Links inside returned Markdown are the same kind of url and are valid read_doc input.`;

const toolAnnotations = {
  readOnlyHint: true,
  destructiveHint: false,
  idempotentHint: true,
  openWorldHint: false,
} as const;

const hitSchema = z.object({
  url: z.string(),
  title: z.string(),
  headingPath: z.array(z.string()),
  excerpt: z.string(),
});

const searchOutputSchema = z.object({ hits: z.array(hitSchema) });

const readOutputSchema = z.object({
  url: z.string(),
  title: z.string(),
  markdown: z.string(),
});

export function renderSearchHits(hits: readonly DocsSearchHit[]): string {
  if (hits.length === 0) {
    return 'No AgentK documentation matches were found.';
  }
  return hits
    .map((hit, index) => {
      const heading =
        hit.headingPath.length > 0 ? ` — ${hit.headingPath.join(' > ')}` : '';
      return `${index + 1}. ${hit.title}${heading}\n${hit.url}\n\n${hit.excerpt}`;
    })
    .join('\n\n');
}

export function createDocsMcpServer(repository: DocsRepository): McpServer {
  const server = new McpServer(
    { name: 'agentk', version: '0.0.0' },
    { instructions: SERVER_INSTRUCTIONS },
  );

  server.registerTool(
    'search_docs',
    {
      title: 'Search AgentK documentation',
      description:
        'Search AgentK concepts, guides, examples, API references, and platform documentation. Each hit carries the absolute page url to cite or read.',
      inputSchema: z.object({
        query: z.string().min(1),
        area: z.enum(SEARCH_AREAS).optional(),
        kind: z.enum(SEARCH_KINDS).optional(),
        language: z.enum(SEARCH_LANGUAGES).optional(),
        limit: z.number().int().min(1).max(20).optional(),
      }),
      outputSchema: searchOutputSchema,
      annotations: toolAnnotations,
    },
    async (input) => {
      const hits = repository.search({
        query: input.query,
        ...(input.area === undefined ? {} : { area: input.area }),
        ...(input.kind === undefined ? {} : { kind: input.kind }),
        ...(input.language === undefined ? {} : { language: input.language }),
        ...(input.limit === undefined ? {} : { limit: input.limit }),
      });
      return {
        content: [{ type: 'text', text: renderSearchHits(hits) }],
        structuredContent: { hits },
      };
    },
  );

  server.registerTool(
    'lookup_api',
    {
      title: 'Look up an AgentK API symbol',
      description:
        'Look up AgentK SDK types, methods, functions, fields, and qualified symbols in the API references.',
      inputSchema: z.object({
        symbol: z.string().min(1),
        language: z.enum(SEARCH_LANGUAGES).optional(),
        limit: z.number().int().min(1).max(20).optional(),
      }),
      outputSchema: searchOutputSchema,
      annotations: toolAnnotations,
    },
    async ({ symbol, language, limit }) => {
      const hits = repository.search({
        query: symbol,
        kind: 'api',
        ...(language === undefined ? {} : { language }),
        ...(limit === undefined ? {} : { limit }),
      });
      return {
        content: [{ type: 'text', text: renderSearchHits(hits) }],
        structuredContent: { hits },
      };
    },
  );

  server.registerTool(
    'read_doc',
    {
      title: 'Read an AgentK documentation page',
      description:
        'Read the complete Markdown of one AgentK documentation page. Takes an absolute page url as returned by search_docs, lookup_api, or a link inside returned Markdown; any #fragment is ignored because the whole page is returned.',
      inputSchema: z.object({ url: z.string().min(1) }),
      outputSchema: readOutputSchema,
      annotations: toolAnnotations,
    },
    async ({ url }) => {
      const document = await repository.read(url);
      return {
        content: [{ type: 'text', text: document.markdown }],
        structuredContent: document,
      };
    },
  );

  return server;
}

export { SERVER_INSTRUCTIONS, toolAnnotations };
