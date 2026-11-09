import { createServer, type Server as NodeHttpServer } from 'node:http';
import { fileURLToPath } from 'node:url';

import compression from 'compression';
import express, { type Express, type RequestHandler } from 'express';
import { toNodeHandler } from '@modelcontextprotocol/node';
import { createMcpHandler } from '@modelcontextprotocol/server';
import {
  authorizationHandler,
  clientRegistrationHandler,
  createOAuthMetadata,
  getOAuthProtectedResourceMetadataUrl,
  mcpAuthMetadataRouter,
  requireBearerAuth,
  revocationHandler,
  tokenHandler,
} from '@modelcontextprotocol/server-legacy/auth';

import {
  openApIdentityVerifier,
  type ApIdentityVerifier,
} from './ap-identity.js';
import { DOCS_READ_SCOPE, InMemoryOAuthProvider } from './auth.js';
import { createCapabilityHandler } from './capabilities.js';
import {
  loadDocsServiceConfig,
  type DocsServiceConfig,
} from './config.js';
import { createDocsMcpServer } from './mcp.js';
import type { AgentKEndpointResolver } from './kess.js';
import { openDocsRepository, type DocsRepository } from './repository.js';
import { mountStaticDocs, openStaticDocs } from './static.js';

export type DocsService = {
  app: Express;
  close(): Promise<void>;
};

export type DocsServiceDependencies = {
  repository?: DocsRepository;
  apIdentity?: ApIdentityVerifier;
  fetch?: typeof fetch;
  resolveAgentKEndpoint?: AgentKEndpointResolver;
  clock?: () => number;
  secret?: () => string;
};

function hostAndOriginValidation(config: DocsServiceConfig): RequestHandler {
  return (request, response, next) => {
    if (
      request.path !== '/healthz' &&
      request.header('host') !== config.publicOrigin.host
    ) {
      response.status(403).type('text/plain').send('Invalid Host header');
      return;
    }
    const origin = request.header('origin');
    if (origin !== undefined && origin !== config.publicOrigin.origin) {
      response.status(403).type('text/plain').send('Invalid Origin header');
      return;
    }
    next();
  };
}

function apAuthorizeMiddleware(verifier: ApIdentityVerifier): RequestHandler {
  return async (request, response, next) => {
    try {
      response.locals.apUser = await verifier.verify(
        request.header('x-ksap-authn-token'),
        request.socket.remoteAddress,
      );
      next();
    } catch {
      response.status(401).type('text/plain').send('Unauthorized');
    }
  };
}

const exactMountedPath: RequestHandler = (request, response, next) => {
  if (request.path !== '/') {
    response.status(404).type('text/plain').send('Not Found');
    return;
  }
  next();
};

export async function createDocsService(
  config: DocsServiceConfig,
  dependencies: DocsServiceDependencies = {},
): Promise<DocsService> {
  const repository =
    dependencies.repository ??
    (await openDocsRepository(config.repositoryRoot, config.publicUrl));
  const apIdentity =
    dependencies.apIdentity ??
    (await openApIdentityVerifier({
      jwksUrl: config.ksapJwksUrl,
      expectedHost: config.publicOrigin.hostname,
    }));
  const oauth = new InMemoryOAuthProvider({
    resource: config.mcpResource,
    ...(dependencies.clock === undefined ? {} : { clock: dependencies.clock }),
    ...(dependencies.secret === undefined ? {} : { secret: dependencies.secret }),
  });
  const staticDocs = await openStaticDocs(config.buildRoot);
  const mcpHandler = createMcpHandler(() => createDocsMcpServer(repository));
  const nodeMcpHandler = toNodeHandler(mcpHandler);
  const app = express();
  const docsBasePath = config.publicUrl.pathname.replace(/\/$/, '');
  const capabilityPath = `${docsBasePath}/_agentk/capabilities`;

  app.disable('x-powered-by');
  app.use(hostAndOriginValidation(config));
  app.use(compression({
    filter: (request, response) =>
      response.getHeader('content-type') !== 'text/event-stream' &&
      compression.filter(request, response),
  }));
  const oauthMetadata = {
    ...createOAuthMetadata({
      provider: oauth,
      issuerUrl: config.publicOrigin,
      scopesSupported: [DOCS_READ_SCOPE],
    }),
    authorization_endpoint: new URL(
      '/oauth/authorize',
      config.publicOrigin,
    ).href,
    token_endpoint: new URL('/oauth/token', config.publicOrigin).href,
    registration_endpoint: new URL(
      '/oauth/register',
      config.publicOrigin,
    ).href,
    revocation_endpoint: new URL('/oauth/revoke', config.publicOrigin).href,
  };
  const oauthRouter = express.Router();
  oauthRouter.use(
    '/authorize',
    exactMountedPath,
    apAuthorizeMiddleware(apIdentity),
    authorizationHandler({
      provider: oauth,
      issuerUrl: config.publicOrigin,
    }),
  );
  oauthRouter.use(
    '/token',
    exactMountedPath,
    tokenHandler({ provider: oauth }),
  );
  oauthRouter.use(
    '/register',
    exactMountedPath,
    clientRegistrationHandler({ clientsStore: oauth.clientsStore }),
  );
  oauthRouter.use(
    '/revoke',
    exactMountedPath,
    revocationHandler({ provider: oauth }),
  );
  app.use('/.well-known/oauth-authorization-server', exactMountedPath);
  app.use('/.well-known/oauth-protected-resource/mcp', exactMountedPath);
  app.use(
    mcpAuthMetadataRouter({
      oauthMetadata,
      resourceServerUrl: config.mcpResource,
      scopesSupported: [DOCS_READ_SCOPE],
      resourceName: 'AgentK',
    }),
  );
  app.use('/oauth', oauthRouter);
  app.all(
    '/mcp',
    requireBearerAuth({
      verifier: oauth,
      requiredScopes: [DOCS_READ_SCOPE],
      resourceMetadataUrl: getOAuthProtectedResourceMetadataUrl(
        config.mcpResource,
      ),
    }),
    (_request, response, next) => {
      response.setHeader('X-Accel-Buffering', 'no');
      next();
    },
    nodeMcpHandler as unknown as RequestHandler,
  );
  app.post(
    capabilityPath,
    express.json({ limit: '8kb', strict: true }),
    createCapabilityHandler({
      kwsServiceName: config.kwsServiceName,
      ...(config.kwsServiceCredential === undefined
        ? {}
        : { kwsServiceCredential: config.kwsServiceCredential }),
      ...(dependencies.fetch === undefined ? {} : { fetch: dependencies.fetch }),
      ...(dependencies.resolveAgentKEndpoint === undefined
        ? {}
        : { resolveAgentKEndpoint: dependencies.resolveAgentKEndpoint }),
    }),
  );
  app.use(
    capabilityPath,
    (
      error: unknown,
      _request: express.Request,
      response: express.Response,
      next: express.NextFunction,
    ) => {
      if (
        error instanceof SyntaxError ||
        (typeof error === 'object' && error !== null && 'type' in error)
      ) {
        response.status(400).json({ error: 'Invalid capability list request' });
        return;
      }
      next(error);
    },
  );
  app.all(`${docsBasePath}/_agentk/*`, (_request, response) => {
    response.status(404).type('text/plain').send('Not Found');
  });
  app.get('/healthz', (_request, response) => {
    response.status(200).type('application/json').send({ status: 'ok' });
  });
  app.all(
    [
      '/authorize',
      '/token',
      '/register',
      '/revoke',
      '/oauth/*',
      '/.well-known/*',
      '/mcp/*',
    ],
    (_request, response) => {
      response.status(404).type('text/plain').send('Not Found');
    },
  );
  mountStaticDocs(app, staticDocs, config.publicUrl.pathname);
  app.use((_request, response) => {
    response.status(404).type('text/plain').send('Not Found');
  });

  return {
    app,
    async close() {
      oauth.close();
      await mcpHandler.close();
    },
  };
}

export async function startDocsService(
  config: DocsServiceConfig = loadDocsServiceConfig(),
): Promise<NodeHttpServer> {
  const service = await createDocsService(config);
  const server = createServer(service.app);
  server.on('close', () => {
    void service.close();
  });
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(config.port, config.host, () => {
      server.off('error', reject);
      resolve();
    });
  });
  console.error(
    `AgentK Docs Service listening on ${config.host}:${config.port}`,
  );
  return server;
}

async function main(): Promise<void> {
  const server = await startDocsService();
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
