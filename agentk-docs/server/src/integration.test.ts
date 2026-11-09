import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';

import {loadSearchDocuments} from './documents.js';
import {search} from './search.js';

const documentsPath = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../generated/search-documents.json',
);

test(
  'fixed acceptance queries rank the expected AgentK documentation',
  {skip: !fs.existsSync(documentsPath)},
  async () => {
    const documents = await loadSearchDocuments(documentsPath);
    const cases: Array<{
      query: string;
      top: number;
      matches: (route: string) => boolean;
    }> = [
      {
        query: 'Session 和 Profile 区别',
        top: 3,
        matches: (route) => route === '/concepts/profile-session',
      },
      {
        query: 'CreateSession Go',
        top: 3,
        matches: (route) =>
          route === '/reference/go/client' ||
          route === '/reference/go/protocol',
      },
      {
        query: 'agentk.worker.invoke_capability',
        top: 1,
        matches: (route) => route === '/reference/python/worker',
      },
      {
        query: 'AgentKClient startExecution',
        top: 3,
        matches: (route) => route.includes('/reference/java/client/'),
      },
      {
        query: 'capability progress heartbeat',
        top: 3,
        matches: (route) =>
          route === '/concepts/capability-execution-progress' ||
          route.startsWith('/worker'),
      },
    ];

    for (const acceptance of cases) {
      const routes = search(documents, {
        query: acceptance.query,
        limit: acceptance.top,
      }).map((hit) => hit.route);
      assert.ok(
        routes.some(acceptance.matches),
        `${acceptance.query} returned ${routes.join(', ')}`,
      );
    }
  },
);
