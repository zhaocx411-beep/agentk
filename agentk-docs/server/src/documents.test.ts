import assert from 'node:assert/strict';
import test from 'node:test';

import {
  SEARCH_DOCUMENTS_FORMAT,
  validateSearchDocuments,
  type SearchDocuments,
} from './documents.js';

function fixture(): SearchDocuments {
  return {
    format: SEARCH_DOCUMENTS_FORMAT,
    pages: [
      {
        route: '/concepts/session',
        markdownPath: '/concepts/session.md',
        title: 'Session',
        description: '',
        sidebarGroup: '核心概念',
        area: 'concepts',
        kind: 'concept',
        language: null,
        sections: [
          {
            id: '/concepts/session#overview',
            anchor: null,
            headingPath: [],
            markdown: 'Session overview',
            codeTerms: ['Session'],
          },
        ],
      },
    ],
  };
}

test('validates a complete search document corpus', () => {
  assert.deepEqual(validateSearchDocuments(fixture()), fixture());
});

test('rejects unknown formats, enum values and malformed pages', () => {
  assert.throws(
    () => validateSearchDocuments({...fixture(), format: 'future'}),
    /format/u,
  );
  const invalidArea = structuredClone(fixture()) as unknown as {
    pages: Array<{area: string}>;
  };
  invalidArea.pages[0]!.area = 'unknown';
  assert.throws(() => validateSearchDocuments(invalidArea), /area/u);
  assert.throws(
    () =>
      validateSearchDocuments({format: SEARCH_DOCUMENTS_FORMAT, pages: [{}]}),
    /route/u,
  );
  const relativeMarkdownPath = fixture();
  relativeMarkdownPath.pages[0]!.markdownPath = 'concepts/session.md';
  assert.throws(
    () => validateSearchDocuments(relativeMarkdownPath),
    /markdownPath/u,
  );
  const emptyCorpus = {format: SEARCH_DOCUMENTS_FORMAT, pages: []};
  assert.throws(() => validateSearchDocuments(emptyCorpus), /pages/u);
  const emptySections = fixture();
  emptySections.pages[0]!.sections = [];
  assert.throws(() => validateSearchDocuments(emptySections), /sections/u);
});
