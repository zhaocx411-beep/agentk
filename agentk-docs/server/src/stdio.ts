import { serveStdio } from '@modelcontextprotocol/server/stdio';

import { loadPublicUrl } from './config.js';
import { createDocsMcpServer } from './mcp.js';
import { REPOSITORY_ROOT } from './paths.js';
import { openDocsRepository } from './repository.js';

async function main(): Promise<void> {
  const repository = await openDocsRepository(
    REPOSITORY_ROOT,
    loadPublicUrl(),
  );
  const handle = serveStdio(() => createDocsMcpServer(repository));

  const close = (): void => {
    void handle.close();
  };
  process.once('SIGINT', close);
  process.once('SIGTERM', close);
}

main().catch((error: unknown) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
