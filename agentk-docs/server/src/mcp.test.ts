import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';

import { Client, InMemoryTransport } from '@modelcontextprotocol/client';

import { createDocsMcpServer } from './mcp.js';
import { REPOSITORY_ROOT } from './paths.js';
import { openDocsRepository } from './repository.js';
import {
  createDocsFixture,
  FIXTURE_SITE_URL,
  type DocsFixture,
} from './test-fixture.js';

type Hit = {
  url: string;
  title: string;
  headingPath: string[];
  excerpt: string;
};

async function connect(
  root: string,
  siteUrl: URL,
): Promise<{ client: Client; close(): Promise<void> }> {
  const repository = await openDocsRepository(root, siteUrl);
  const server = createDocsMcpServer(repository);
  const client = new Client({ name: 'agentk-docs-test', version: '0.0.0' });
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  await Promise.all([
    client.connect(clientTransport),
    server.server.connect(serverTransport),
  ]);
  return {
    client,
    close: async () => {
      await client.close();
      await server.close();
    },
  };
}

function hits(result: { structuredContent?: unknown }): Hit[] {
  return (result.structuredContent as { hits: Hit[] }).hits;
}

test('exposes exactly three read-only tools and no resources', async (t) => {
  const fixture = await createDocsFixture();
  t.after(fixture.cleanup);
  const connection = await connect(fixture.root, FIXTURE_SITE_URL);
  t.after(connection.close);

  const { tools } = await connection.client.listTools();
  assert.deepEqual(
    tools.map((tool) => tool.name),
    ['search_docs', 'lookup_api', 'read_doc'],
  );
  for (const tool of tools) {
    assert.deepEqual(tool.annotations, {
      readOnlyHint: true,
      destructiveHint: false,
      idempotentHint: true,
      openWorldHint: false,
    });
    assert.equal(tool.inputSchema.type, 'object');
    assert.equal(tool.outputSchema?.type, 'object');
  }
  assert.match(connection.client.getInstructions() ?? '', /absolute https URL/u);
  assert.equal(
    connection.client.getServerCapabilities()?.resources,
    undefined,
  );
});

test('search and lookup return absolute page URLs in text and structured output', async (t) => {
  const fixture = await createDocsFixture();
  t.after(fixture.cleanup);
  const connection = await connect(fixture.root, FIXTURE_SITE_URL);
  t.after(connection.close);

  const search = await connection.client.callTool({
    name: 'search_docs',
    arguments: { query: 'Session', limit: 1 },
  });
  assert.equal(search.isError, undefined);
  const [hit] = hits(search);
  assert.equal(
    hit?.url,
    'https://docs.test/docs/concepts/profile-session#session',
  );
  assert.deepEqual(Object.keys(hit!).sort(), [
    'excerpt',
    'headingPath',
    'title',
    'url',
  ]);
  assert.equal(search.content.length, 1);
  const rendered = search.content[0];
  assert.ok(rendered && rendered.type === 'text');
  assert.ok(rendered.text.includes(hit!.url));

  const missing = await connection.client.callTool({
    name: 'lookup_api',
    arguments: { symbol: 'Session' },
  });
  assert.deepEqual(missing.structuredContent, { hits: [] });

  const symbol = await connection.client.callTool({
    name: 'lookup_api',
    arguments: { symbol: 'agentk.worker.invoke_capability', language: 'python' },
  });
  assert.equal(
    hits(symbol)[0]?.url,
    'https://docs.test/docs/reference/python/worker#invoke-capability',
  );
});

test('a link inside returned Markdown is valid read_doc input', async (t) => {
  const target = 'https://docs.test/docs/reference/python/worker';
  const fixture: DocsFixture = await createDocsFixture(undefined, {
    '/concepts/profile-session.md': [
      '# Profile、Session 与 Execution',
      '',
      `Session 表示持续上下文,能力签名见 [Python Worker API](${target}#invoke-capability)。`,
    ].join('\n'),
  });
  t.after(fixture.cleanup);
  const connection = await connect(fixture.root, FIXTURE_SITE_URL);
  t.after(connection.close);

  const search = await connection.client.callTool({
    name: 'search_docs',
    arguments: { query: 'Session', limit: 1 },
  });
  const page = await connection.client.callTool({
    name: 'read_doc',
    arguments: { url: hits(search)[0]!.url },
  });
  const markdown = (page.structuredContent as { markdown: string }).markdown;

  const link = /\]\((https?:\/\/[^)]+)\)/u.exec(markdown)?.[1];
  assert.equal(link, `${target}#invoke-capability`);

  const followed = await connection.client.callTool({
    name: 'read_doc',
    arguments: { url: link },
  });
  assert.equal(followed.isError, undefined);
  const document = followed.structuredContent as {
    url: string;
    title: string;
    markdown: string;
  };
  assert.equal(document.url, target);
  assert.equal(document.title, 'Python Worker API');
  assert.match(document.markdown, /invoke_capability/u);
});

test('rejects invalid tool input and unknown pages', async (t) => {
  const fixture = await createDocsFixture();
  t.after(fixture.cleanup);
  const connection = await connect(fixture.root, FIXTURE_SITE_URL);
  t.after(connection.close);

  const invalid = await connection.client.callTool({
    name: 'search_docs',
    arguments: { query: 'Session', limit: 21 },
  });
  assert.equal(invalid.isError, true);
  assert.match(
    invalid.content[0]?.type === 'text' ? invalid.content[0].text : '',
    /Input validation error/u,
  );

  for (const [url, message] of [
    ['https://docs.test/docs/missing', /Unknown AgentK documentation page/u],
    ['/concepts/profile-session.md', /Not an absolute AgentK documentation URL/u],
  ] as const) {
    const failed = await connection.client.callTool({
      name: 'read_doc',
      arguments: { url },
    });
    assert.equal(failed.isError, true, url);
    assert.match(
      failed.content[0]?.type === 'text' ? failed.content[0].text : '',
      message,
      url,
    );
  }
});

test(
  'runs the fixed acceptance queries through the MCP tool surface',
  {
    skip: !fs.existsSync(
      path.join(REPOSITORY_ROOT, 'generated/search-documents.json'),
    ),
  },
  async (t) => {
    const siteUrl = new URL('https://agentk.test/docs/');
    const connection = await connect(REPOSITORY_ROOT, siteUrl);
    t.after(connection.close);
    const cases = [
      { query: 'Session 和 Profile 区别', top: 3, url: /\/concepts\/profile-session/u },
      { query: 'CreateSession Go', top: 3, url: /\/reference\/go\/(client|protocol)/u },
      { query: 'agentk.worker.invoke_capability', top: 1, url: /\/reference\/python\/worker/u },
      { query: 'AgentKClient startExecution', top: 3, url: /\/reference\/java\/client\//u },
      { query: 'capability progress heartbeat', top: 3, url: /(capability-execution-progress|\/worker)/u },
    ];

    for (const acceptance of cases) {
      const result = await connection.client.callTool({
        name: 'search_docs',
        arguments: { query: acceptance.query, limit: acceptance.top },
      });
      const found = hits(result);
      assert.ok(
        found.every((hit) => hit.url.startsWith(siteUrl.href)),
        `${acceptance.query} returned ${found.map((hit) => hit.url).join(', ')}`,
      );
      assert.ok(
        found.some((hit) => acceptance.url.test(hit.url)),
        `${acceptance.query} returned ${found.map((hit) => hit.url).join(', ')}`,
      );
    }
  },
);
