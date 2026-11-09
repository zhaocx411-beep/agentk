import assert from 'node:assert/strict';
import test from 'node:test';

import {SEARCH_DOCUMENTS_FORMAT, type SearchDocuments} from './documents.js';
import {EXCERPT_MAX_CHARACTERS, search} from './search.js';

const documents: SearchDocuments = {
  format: SEARCH_DOCUMENTS_FORMAT,
  pages: [
    {
      route: '/concepts/profile-session',
      markdownPath: '/concepts/profile-session.md',
      title: 'Profile、Session 与 Execution',
      description: '',
      sidebarGroup: '核心概念',
      area: 'concepts',
      kind: 'concept',
      language: null,
      sections: [
        {
          id: '/concepts/profile-session#session',
          anchor: 'session',
          headingPath: ['Session'],
          markdown: `## Session\n\n${'x'.repeat(700)} Session 表示持续上下文。`,
          codeTerms: ['Session', 'session_id'],
        },
      ],
    },
    {
      route: '/reference/python/worker',
      markdownPath: '/reference/python/worker.md',
      title: 'Python Worker API',
      description: '',
      sidebarGroup: 'Python',
      area: 'reference',
      kind: 'api',
      language: 'python',
      sections: [
        {
          id: '/reference/python/worker#invoke',
          anchor: 'invoke',
          headingPath: ['invoke_capability'],
          markdown:
            '## `invoke_capability`\n\nCall `agentk.worker.invoke_capability`.',
          codeTerms: ['invoke_capability', 'agentk.worker.invoke_capability'],
        },
      ],
    },
  ],
};

test('scores exact code terms and applies filters before ranking', () => {
  const hits = search(documents, {
    query: 'agentk.worker.invoke_capability',
    language: 'python',
    kind: 'api',
  });
  assert.equal(hits[0]?.route, '/reference/python/worker');
  assert.ok((hits[0]?.score ?? 0) >= 50);
  assert.deepEqual(search(documents, {query: 'Session', language: 'go'}), []);
});

test('rejects empty queries and invalid limits', () => {
  assert.throws(() => search(documents, {query: '  '}), /must not be empty/u);
  assert.throws(
    () => search(documents, {query: 'Session', limit: 21}),
    /between 1 and 20/u,
  );
  assert.throws(
    () => search(documents, {query: 'Session', limit: 1.5}),
    /integer/u,
  );
});

test('uses deterministic page and section order for ties', () => {
  const tied = structuredClone(documents);
  tied.pages.push({
    ...structuredClone(tied.pages[0]!),
    route: '/concepts/later',
    markdownPath: '/concepts/later.md',
    sections: [
      {
        ...structuredClone(tied.pages[0]!.sections[0]!),
        id: '/concepts/later#session',
      },
    ],
  });
  assert.deepEqual(
    search(tied, {query: 'Session'}).map((hit) => hit.route),
    ['/concepts/profile-session', '/concepts/later'],
  );
});

test('limits results and creates a bounded excerpt around the best match', () => {
  const [hit] = search(documents, {query: '持续上下文', limit: 1});
  assert.ok(hit);
  assert.ok(hit.excerpt.includes('持续上下文'));
  assert.ok(hit.excerpt.length <= EXCERPT_MAX_CHARACTERS);
  assert.equal(search(documents, {query: 'Session', limit: 1}).length, 1);
});

test('never truncates an excerpt into an unbalanced fenced block', () => {
  const fenced: SearchDocuments = {
    format: SEARCH_DOCUMENTS_FORMAT,
    pages: [
      {
        route: '/client/quickstart',
        markdownPath: '/client/quickstart.md',
        title: 'Quickstart',
        description: '',
        sidebarGroup: '开始',
        area: 'client',
        kind: 'guide',
        language: null,
        sections: [
          {
            id: '/client/quickstart#run',
            anchor: 'run',
            headingPath: ['运行'],
            markdown: [
              '## 运行',
              '',
              '```python',
              ...Array.from(
                {length: 40},
                (_unused, line) => `print("padding ${line}")`,
              ),
              'create_session(title="needle")',
              ...Array.from(
                {length: 40},
                (_unused, line) => `print("tail ${line}")`,
              ),
              '```',
              '',
              `${'trailing prose. '.repeat(40)}`,
            ].join('\n'),
            codeTerms: ['create_session', 'needle'],
          },
        ],
      },
    ],
  };

  const [hit] = search(fenced, {query: 'needle', limit: 1});
  assert.ok(hit);
  assert.ok(hit.excerpt.includes('needle'));
  assert.ok(hit.excerpt.length <= EXCERPT_MAX_CHARACTERS);
  assert.equal(
    (hit.excerpt.match(/^ {0,3}(?:`{3,}|~{3,})/gmu) ?? []).length % 2,
    0,
  );
  assert.ok(hit.excerpt.startsWith('```python'));
  assert.ok(hit.excerpt.endsWith('```'));
});
