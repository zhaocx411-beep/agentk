import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';

import {
  SEARCH_DOCUMENTS_FORMAT,
  type SearchDocuments,
  type SearchPage,
} from './documents.js';

export const FIXTURE_SITE_URL = new URL('https://docs.test/docs/');

export const fixturePages: SearchPage[] = [
  {
    route: '/concepts/profile-session',
    markdownPath: '/concepts/profile-session.md',
    title: 'Profile、Session 与 Execution',
    description: '理解配置、会话和执行的生命周期边界',
    sidebarGroup: '核心概念',
    area: 'concepts',
    kind: 'concept',
    language: null,
    sections: [
      {
        id: '/concepts/profile-session#session',
        anchor: 'session',
        headingPath: ['Session'],
        markdown: '## Session\n\nSession 表示持续的 Agent 交互上下文。',
        codeTerms: ['Session', 'session_id'],
      },
    ],
  },
  {
    route: '/reference/python/worker',
    markdownPath: '/reference/python/worker.md',
    title: 'Python Worker API',
    description: 'Python Worker API reference',
    sidebarGroup: 'Python',
    area: 'reference',
    kind: 'api',
    language: 'python',
    sections: [
      {
        id: '/reference/python/worker#invoke-capability',
        anchor: 'invoke-capability',
        headingPath: ['invoke_capability'],
        markdown: '## invoke_capability\n\nCall `agentk.worker.invoke_capability`.',
        codeTerms: ['invoke_capability', 'agentk.worker.invoke_capability'],
      },
    ],
  },
];

export type DocsFixture = {
  root: string;
  cleanup(): Promise<void>;
};

export async function createDocsFixture(
  pages: SearchPage[] = structuredClone(fixturePages),
  markdownOverrides: Readonly<Record<string, string>> = {},
): Promise<DocsFixture> {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'agentk-docs-mcp-'));
  await fs.mkdir(path.join(root, 'generated'), { recursive: true });
  await fs.mkdir(path.join(root, 'build'), { recursive: true });

  const documents: SearchDocuments = {
    format: SEARCH_DOCUMENTS_FORMAT,
    pages,
  };
  await fs.writeFile(
    path.join(root, 'generated', 'search-documents.json'),
    `${JSON.stringify(documents)}\n`,
  );
  for (const page of pages) {
    const diskPath = path.join(root, 'build', page.markdownPath.slice(1));
    await fs.mkdir(path.dirname(diskPath), { recursive: true });
    await fs.writeFile(
      diskPath,
      markdownOverrides[page.markdownPath] ?? `# ${page.title}\n\n${page.sections.map((section) => section.markdown).join('\n\n')}`,
    );
  }

  return {
    root,
    cleanup: () => fs.rm(root, { recursive: true, force: true }),
  };
}
