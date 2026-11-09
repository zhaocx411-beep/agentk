import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { createServer, request as httpRequest } from 'node:http';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import express from 'express';

import { mountStaticDocs, openStaticDocs } from './static.js';

function rawStatus(url: URL, headers: Record<string, string>): Promise<number> {
  return new Promise((resolve, reject) => {
    const request = httpRequest(url, { headers }, (response) => {
      response.resume();
      resolve(response.statusCode ?? 0);
    });
    request.once('error', reject);
    request.end();
  });
}

test('static docs preserve caching, HEAD, conditional requests, and 404', async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'agentk-static-'));
  await fs.mkdir(path.join(directory, 'guide'), { recursive: true });
  await fs.mkdir(path.join(directory, 'assets', 'js'), { recursive: true });
  await fs.writeFile(path.join(directory, 'index.html'), '<h1>Home</h1>');
  await fs.writeFile(path.join(directory, 'guide', 'index.html'), '<h1>Guide</h1>');
  await fs.writeFile(path.join(directory, 'guide.md'), '# Guide');
  await fs.writeFile(path.join(directory, 'llms.txt'), '# LLM');
  await fs.writeFile(path.join(directory, '404.html'), '<h1>Missing</h1>');
  await fs.writeFile(
    path.join(directory, 'assets', 'js', 'main.0123456789abcdef.js'),
    'console.log(1)',
  );
  const outside = path.join(directory, '..', `${path.basename(directory)}-secret`);
  await fs.writeFile(outside, 'secret');
  await fs.symlink(outside, path.join(directory, 'escape.txt'));

  const app = express();
  mountStaticDocs(app, await openStaticDocs(directory), '/docs/');
  const server = createServer(app);
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const origin = `http://127.0.0.1:${address.port}`;
  try {
    const baseRedirect = await fetch(`${origin}/docs`, { redirect: 'manual' });
    assert.equal(baseRedirect.status, 308);
    assert.equal(baseRedirect.headers.get('location'), '/docs/');

    const html = await fetch(`${origin}/docs/guide/`);
    assert.equal(html.status, 200);
    assert.equal(html.headers.get('cache-control'), 'no-cache');
    const etag = html.headers.get('etag');
    assert.ok(etag);
    assert.equal(
      await rawStatus(new URL('/docs/guide/', origin), { 'if-none-match': etag }),
      304,
    );

    const markdown = await fetch(`${origin}/docs/guide.md`, { method: 'HEAD' });
    assert.equal(markdown.status, 200);
    assert.equal(markdown.headers.get('cache-control'), 'no-cache');
    assert.equal(await markdown.text(), '');

    const asset = await fetch(
      `${origin}/docs/assets/js/main.0123456789abcdef.js`,
    );
    assert.equal(
      asset.headers.get('cache-control'),
      'public, max-age=31536000, immutable',
    );
    const missing = await fetch(`${origin}/docs/not-here`);
    assert.equal(missing.status, 404);
    assert.match(await missing.text(), /Missing/);
    const escaped = await fetch(`${origin}/docs/escape.txt`);
    assert.equal(escaped.status, 403);
    assert.equal((await fetch(`${origin}/guide/`)).status, 404);
  } finally {
    const closed = new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
    server.closeAllConnections();
    await closed;
    await fs.rm(directory, { recursive: true });
    await fs.rm(outside);
  }
});
