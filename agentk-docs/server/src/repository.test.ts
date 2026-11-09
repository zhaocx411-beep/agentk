import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';

import { MAX_DOCUMENT_BYTES, openDocsRepository, toPageUrl } from './repository.js';
import {
  createDocsFixture,
  fixturePages,
  FIXTURE_SITE_URL,
} from './test-fixture.js';

test('maps every indexed page to one absolute site URL', async (t) => {
  const fixture = await createDocsFixture();
  t.after(fixture.cleanup);
  const repository = await openDocsRepository(fixture.root, FIXTURE_SITE_URL);

  assert.deepEqual(repository.urls, [
    'https://docs.test/docs/concepts/profile-session',
    'https://docs.test/docs/reference/python/worker',
  ]);
  const [hit] = repository.search({ query: 'Session', limit: 1 });
  assert.equal(
    hit?.url,
    'https://docs.test/docs/concepts/profile-session#session',
  );

  const document = await repository.read(hit!.url);
  assert.equal(document.url, 'https://docs.test/docs/concepts/profile-session');
  assert.match(document.markdown, /Session 表示持续/u);
});

test('reads a page URL with a fragment or a trailing slash', async (t) => {
  const fixture = await createDocsFixture();
  t.after(fixture.cleanup);
  const repository = await openDocsRepository(fixture.root, FIXTURE_SITE_URL);

  for (const url of [
    'https://docs.test/docs/concepts/profile-session',
    'https://docs.test/docs/concepts/profile-session#session',
    'https://docs.test/docs/concepts/profile-session/',
  ]) {
    const document = await repository.read(url);
    assert.equal(
      document.url,
      'https://docs.test/docs/concepts/profile-session',
      url,
    );
  }
});

test('rejects unknown, relative, and foreign page URLs', async (t) => {
  const fixture = await createDocsFixture();
  t.after(fixture.cleanup);
  const repository = await openDocsRepository(fixture.root, FIXTURE_SITE_URL);

  await assert.rejects(
    repository.read('https://docs.test/docs/not-indexed'),
    /Unknown AgentK documentation page/u,
  );
  await assert.rejects(
    repository.read('/docs/concepts/profile-session'),
    /Not an absolute AgentK documentation URL/u,
  );
  await assert.rejects(
    repository.read('https://evil.example/docs/concepts/profile-session'),
    /Not an AgentK documentation URL/u,
  );
});

test('refuses to index a page larger than the read limit', async (t) => {
  const fixture = await createDocsFixture(undefined, {
    '/reference/python/worker.md': 'x'.repeat(MAX_DOCUMENT_BYTES + 1),
  });
  t.after(fixture.cleanup);

  await assert.rejects(
    openDocsRepository(fixture.root, FIXTURE_SITE_URL),
    /80 KiB read limit/u,
  );
});

test('rejects duplicate and traversal-like indexed pages', async (t) => {
  const duplicatePages = structuredClone(fixturePages);
  duplicatePages[1]!.route = duplicatePages[0]!.route;
  const duplicate = await createDocsFixture(duplicatePages);
  t.after(duplicate.cleanup);
  await assert.rejects(
    openDocsRepository(duplicate.root, FIXTURE_SITE_URL),
    /Duplicate indexed documentation page/u,
  );

  const traversalPages = structuredClone(fixturePages.slice(0, 1));
  traversalPages[0]!.markdownPath = '/../outside.md';
  const traversal = await createDocsFixture(traversalPages);
  t.after(traversal.cleanup);
  await assert.rejects(
    openDocsRepository(traversal.root, FIXTURE_SITE_URL),
    /Invalid indexed Markdown path/u,
  );
});

test('rejects an indexed symlink that escapes the build directory', async (t) => {
  const pages = structuredClone(fixturePages.slice(0, 1));
  pages[0]!.markdownPath = '/escape.md';
  const fixture = await createDocsFixture(pages);
  t.after(fixture.cleanup);

  const outside = path.join(fixture.root, 'outside.md');
  await fs.writeFile(outside, '# Outside');
  await fs.rm(path.join(fixture.root, 'build', 'escape.md'));
  await fs.symlink(outside, path.join(fixture.root, 'build', 'escape.md'));

  await assert.rejects(
    openDocsRepository(fixture.root, FIXTURE_SITE_URL),
    /resolves outside the documentation build directory/u,
  );
});

test('builds page URLs the same way the build-time link rewriter does', () => {
  assert.equal(
    toPageUrl('/reference/go/client', FIXTURE_SITE_URL),
    'https://docs.test/docs/reference/go/client',
  );
  assert.equal(toPageUrl('/', FIXTURE_SITE_URL), 'https://docs.test/docs/');
});
