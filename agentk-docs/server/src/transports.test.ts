import assert from 'node:assert/strict';
import { request as httpRequest } from 'node:http';
import type { AddressInfo } from 'node:net';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

import { Client, StreamableHTTPClientTransport } from '@modelcontextprotocol/client';
import { StdioClientTransport } from '@modelcontextprotocol/client/stdio';

import { createDocsHttpServer, HTTP_HOST } from './http.js';
import { openDocsRepository } from './repository.js';
import { createDocsFixture, FIXTURE_SITE_URL } from './test-fixture.js';

type ProtocolMode = 'legacy' | 'auto';

function createClient(mode: ProtocolMode): Client {
  return new Client(
    { name: `agentk-docs-${mode}-test`, version: '0.0.0' },
    { versionNegotiation: { mode } },
  );
}

async function startHttpFixture(): Promise<{
  baseUrl: URL;
  close(): Promise<void>;
}> {
  const fixture = await createDocsFixture();
  const repository = await openDocsRepository(fixture.root, FIXTURE_SITE_URL);
  const server = createDocsHttpServer(repository);
  try {
    await new Promise<void>((resolve, reject) => {
      server.once('error', reject);
      server.listen(0, HTTP_HOST, resolve);
    });
  } catch (error) {
    await fixture.cleanup();
    throw error;
  }
  const port = (server.address() as AddressInfo).port;
  // noinspection HttpUrlsUsage -- the server is deliberately loopback-only.
  const baseUrl = new URL(`http://${HTTP_HOST}:${port}`);
  return {
    baseUrl,
    close: async () => {
      await new Promise<void>((resolve) => server.close(() => resolve()));
      await fixture.cleanup();
    },
  };
}

async function requestStatus(
  url: URL,
  headers: Readonly<Record<string, string>> = {},
): Promise<number> {
  return new Promise<number>((resolve, reject) => {
    const request = httpRequest(
      {
        hostname: url.hostname,
        port: url.port,
        path: url.pathname,
        method: 'POST',
        headers: { 'content-type': 'application/json', ...headers },
      },
      (response) => {
        response.resume();
        resolve(response.statusCode ?? 0);
      },
    );
    request.once('error', reject);
    request.end('{}');
  });
}

test('serves legacy and modern MCP clients over stdio without stdout noise', async () => {
  const stdioPath = path.join(
    path.dirname(fileURLToPath(import.meta.url)),
    'stdio.js',
  );
  for (const mode of ['legacy', 'auto'] as const) {
    const client = createClient(mode);
    const transport = new StdioClientTransport({
      command: process.execPath,
      args: [stdioPath],
      env: { AGENTK_DOCS_PUBLIC_URL: 'https://agentk.test/docs/' },
      stderr: 'pipe',
    });
    await client.connect(transport);
    assert.equal(client.getProtocolEra(), mode === 'auto' ? 'modern' : 'legacy');
    const { tools } = await client.listTools();
    assert.deepEqual(
      tools.map((tool) => tool.name),
      ['search_docs', 'lookup_api', 'read_doc'],
    );
    const result = await client.callTool({
      name: 'search_docs',
      arguments: { query: 'Session 和 Profile 区别', limit: 3 },
    });
    assert.equal(result.isError, undefined);
    await client.close();
  }
});

test('serves legacy and modern clients concurrently over loopback HTTP', async (t) => {
  const fixture = await startHttpFixture();
  t.after(fixture.close);
  const url = new URL('/mcp', fixture.baseUrl);

  await Promise.all(
    (['legacy', 'auto'] as const).map(async (mode) => {
      const client = createClient(mode);
      await client.connect(new StreamableHTTPClientTransport(url));
      assert.equal(client.getProtocolEra(), mode === 'auto' ? 'modern' : 'legacy');
      const search = await client.callTool({
        name: 'search_docs',
        arguments: { query: 'Session', limit: 1 },
      });
      const [hit] = (search.structuredContent as { hits: Array<{ url: string }> })
        .hits;
      assert.equal(
        hit?.url,
        'https://docs.test/docs/concepts/profile-session#session',
      );
      const page = await client.callTool({
        name: 'read_doc',
        arguments: { url: hit!.url },
      });
      assert.equal(page.isError, undefined);
      await client.close();
    }),
  );
});

test('HTTP endpoint rejects non-MCP paths, hostile Host, and hostile Origin', async (t) => {
  const fixture = await startHttpFixture();
  t.after(fixture.close);

  assert.equal((await fetch(new URL('/not-mcp', fixture.baseUrl))).status, 404);
  assert.equal(
    await requestStatus(new URL('/mcp', fixture.baseUrl), { host: 'evil.example' }),
    403,
  );
  assert.equal(
    await requestStatus(new URL('/mcp', fixture.baseUrl), {
      origin: 'https://evil.example',
    }),
    403,
  );
});
