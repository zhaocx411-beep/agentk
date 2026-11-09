import { createServer, type Server as NodeHttpServer } from 'node:http';
import { fileURLToPath } from 'node:url';

import { localhostHostValidation, localhostOriginValidation, toNodeHandler } from '@modelcontextprotocol/node';
import { createMcpHandler } from '@modelcontextprotocol/server';

import { loadPublicUrl } from './config.js';
import { createDocsMcpServer } from './mcp.js';
import { REPOSITORY_ROOT } from './paths.js';
import { DocsRepository, openDocsRepository } from './repository.js';

export const HTTP_HOST = '127.0.0.1';
export const HTTP_PORT = 3100;

export function createDocsHttpServer(repository: DocsRepository): NodeHttpServer {
  const handler = createMcpHandler(() => createDocsMcpServer(repository));
  const nodeHandler = toNodeHandler(handler);
  const validateHost = localhostHostValidation();
  const validateOrigin = localhostOriginValidation();

  const server = createServer((request, response) => {
    if (request.method === undefined || request.url === undefined) {
      response.writeHead(400, { 'content-type': 'text/plain; charset=utf-8' });
      response.end('Bad Request');
      return;
    }
    const completeRequest = request as typeof request & {
      method: string;
      url: string;
    };
    const pathname = new URL(completeRequest.url, 'http://localhost').pathname;
    if (pathname !== '/mcp') {
      response.writeHead(404, { 'content-type': 'text/plain; charset=utf-8' });
      response.end('Not Found');
      return;
    }
    if (!validateHost(request, response) || !validateOrigin(request, response)) {
      return;
    }
    void nodeHandler(completeRequest, response);
  });

  server.on('close', () => {
    void handler.close();
  });
  return server;
}

async function main(): Promise<void> {
  const repository = await openDocsRepository(
    REPOSITORY_ROOT,
    loadPublicUrl(),
  );
  const server = createDocsHttpServer(repository);
  server.listen(HTTP_PORT, HTTP_HOST, () => {
    // noinspection HttpUrlsUsage -- this batch intentionally exposes loopback HTTP only.
    console.error(`AgentK Docs MCP listening at http://${HTTP_HOST}:${HTTP_PORT}/mcp`);
  });

  const close = (): void => {
    server.close();
  };
  process.once('SIGINT', close);
  process.once('SIGTERM', close);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main().catch((error: unknown) => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  });
}
