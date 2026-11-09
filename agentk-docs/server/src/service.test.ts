import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { createServer, request as httpRequest } from 'node:http';
import type { AddressInfo } from 'node:net';
import path from 'node:path';
import test from 'node:test';

import { Client, StreamableHTTPClientTransport } from '@modelcontextprotocol/client';

import type { ApIdentityVerifier } from './ap-identity.js';
import type { DocsServiceConfig } from './config.js';
import { REPOSITORY_ROOT } from './paths.js';
import { createDocsService } from './service.js';

const AP_IDENTITY: ApIdentityVerifier = {
  async verify(rawToken) {
    if (rawToken !== 'valid-ap-token') throw new Error('invalid AP token');
    return { username: 'alice', displayName: 'Alice' };
  },
};

function serviceConfig(publicOrigin: URL): DocsServiceConfig {
  return {
    publicUrl: new URL('/docs/', publicOrigin),
    publicOrigin,
    mcpResource: new URL('/mcp', publicOrigin),
    kwsServiceName: 'klingai-agentk-docs',
    kwsServiceCredential: 'ksn-credential',
    ksapJwksUrl: 'https://ksap.test/jwks',
    repositoryRoot: REPOSITORY_ROOT,
    buildRoot: path.join(REPOSITORY_ROOT, 'build'),
    host: '127.0.0.1',
    port: 8080,
  };
}

async function startFixture(): Promise<{
  origin: URL;
  publicOrigin: URL;
  close(): Promise<void>;
}> {
  const reservation = createServer();
  await new Promise<void>((resolve) => reservation.listen(0, '127.0.0.1', resolve));
  const port = (reservation.address() as AddressInfo).port;
  await new Promise<void>((resolve) => reservation.close(() => resolve()));
  const publicOrigin = new URL(`http://127.0.0.1:${port}`);
  const service = await createDocsService(serviceConfig(publicOrigin), {
    apIdentity: AP_IDENTITY,
    fetch: async () => Response.json({ capabilities: [] }),
    resolveAgentKEndpoint: async () => new URL('http://agentk.internal/'),
  });
  const server = createServer(service.app);
  await new Promise<void>((resolve) => server.listen(port, '127.0.0.1', resolve));
  return {
    origin: publicOrigin,
    publicOrigin,
    close: async () => {
      const closed = new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      );
      server.closeAllConnections();
      await closed;
      await service.close();
    },
  };
}

function request(
  fixtureOrigin: URL,
  pathname: string,
  init: RequestInit = {},
): Promise<Response> {
  return fetch(new URL(pathname, fixtureOrigin), init);
}

function rawStatus(
  origin: URL,
  pathname: string,
  headers: Record<string, string>,
): Promise<number> {
  return new Promise((resolve, reject) => {
    const outgoing = httpRequest(
      new URL(pathname, origin),
      { method: 'POST', headers },
      (response) => {
        response.resume();
        resolve(response.statusCode ?? 0);
      },
    );
    outgoing.once('error', reject);
    outgoing.end('{}');
  });
}

async function registerClient(origin: URL): Promise<{ client_id: string }> {
  const response = await request(origin, '/oauth/register', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      client_name: 'AgentK Docs E2E',
      redirect_uris: ['http://127.0.0.1:49152/callback'],
      token_endpoint_auth_method: 'none',
      grant_types: ['authorization_code', 'refresh_token'],
      response_types: ['code'],
    }),
  });
  if (response.status !== 201) {
    assert.fail(`DCR returned ${response.status}: ${await response.text()}`);
  }
  return (await response.json()) as { client_id: string };
}

async function authorizeAndExchange(
  origin: URL,
  clientId: string,
): Promise<{ access_token: string; refresh_token: string }> {
  const verifier = 'a'.repeat(64);
  const challenge = createHash('sha256')
    .update(verifier)
    .digest('base64url');
  const authorize = new URL('/oauth/authorize', origin);
  authorize.searchParams.set('response_type', 'code');
  authorize.searchParams.set('client_id', clientId);
  authorize.searchParams.set(
    'redirect_uri',
    'http://127.0.0.1:49152/callback',
  );
  authorize.searchParams.set('code_challenge', challenge);
  authorize.searchParams.set('code_challenge_method', 'S256');
  authorize.searchParams.set('scope', 'agentk.docs.read');
  authorize.searchParams.set('resource', new URL('/mcp', origin).href);
  authorize.searchParams.set('state', 'state-value');
  const authorization = await request(origin, authorize.pathname + authorize.search, {
    headers: { 'x-ksap-authn-token': 'valid-ap-token' },
    redirect: 'manual',
  });
  if (authorization.status !== 302) {
    assert.fail(
      `Authorization returned ${authorization.status}: ${await authorization.text()}`,
    );
  }
  const redirect = new URL(authorization.headers.get('location')!);
  assert.equal(redirect.searchParams.get('state'), 'state-value');
  assert.equal(redirect.searchParams.get('iss'), origin.href);
  const code = redirect.searchParams.get('code');
  assert.ok(code);

  const token = await request(origin, '/oauth/token', {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      grant_type: 'authorization_code',
      client_id: clientId,
      code,
      code_verifier: verifier,
      redirect_uri: 'http://127.0.0.1:49152/callback',
      resource: new URL('/mcp', origin).href,
    }),
  });
  if (token.status !== 200) {
    assert.fail(`Token exchange returned ${token.status}: ${await token.text()}`);
  }
  return (await token.json()) as {
    access_token: string;
    refresh_token: string;
  };
}

test('OAuth metadata and unauthenticated MCP response drive discovery', async (t) => {
  const fixture = await startFixture();
  t.after(fixture.close);
  const unauthorized = await request(fixture.origin, '/mcp', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: '{}',
  });
  assert.equal(unauthorized.status, 401);
  assert.match(
    unauthorized.headers.get('www-authenticate') ?? '',
    /oauth-protected-resource\/mcp/,
  );
  const resourceMetadata = await request(
    fixture.origin,
    '/.well-known/oauth-protected-resource/mcp',
  );
  assert.equal(resourceMetadata.status, 200);
  const resource = (await resourceMetadata.json()) as {
    resource: string;
    authorization_servers: string[];
  };
  assert.equal(resource.resource, new URL('/mcp', fixture.origin).href);
  assert.deepEqual(resource.authorization_servers, [fixture.origin.href]);

  const authorizationMetadata = await request(
    fixture.origin,
    '/.well-known/oauth-authorization-server',
  );
  assert.equal(authorizationMetadata.status, 200);
  assert.deepEqual(
    await authorizationMetadata.json(),
    {
      issuer: fixture.origin.href,
      authorization_endpoint: new URL(
        '/oauth/authorize',
        fixture.origin,
      ).href,
      response_types_supported: ['code'],
      code_challenge_methods_supported: ['S256'],
      token_endpoint: new URL('/oauth/token', fixture.origin).href,
      token_endpoint_auth_methods_supported: ['client_secret_post', 'none'],
      grant_types_supported: ['authorization_code', 'refresh_token'],
      scopes_supported: ['agentk.docs.read'],
      revocation_endpoint: new URL('/oauth/revoke', fixture.origin).href,
      revocation_endpoint_auth_methods_supported: ['client_secret_post'],
      registration_endpoint: new URL('/oauth/register', fixture.origin).href,
      authorization_response_iss_parameter_supported: true,
    },
  );

  const missingIdentity = await request(
    fixture.origin,
    '/oauth/authorize?response_type=code',
  );
  assert.equal(missingIdentity.status, 401);
  assert.equal(
    await rawStatus(fixture.origin, '/mcp', {
      host: 'evil.example',
      'content-type': 'application/json',
    }),
    403,
  );
  const hostileOrigin = await request(fixture.origin, '/mcp', {
    method: 'POST',
    headers: {
      origin: 'https://evil.example',
      'content-type': 'application/json',
    },
    body: '{}',
  });
  assert.equal(hostileOrigin.status, 403);
});

test('DCR, AP-backed authorization, PKCE, and MCP work end-to-end', async (t) => {
  const fixture = await startFixture();
  t.after(fixture.close);
  const clientRegistration = await registerClient(fixture.origin);
  const tokens = await authorizeAndExchange(
    fixture.origin,
    clientRegistration.client_id,
  );
  const refresh = await request(fixture.origin, '/oauth/token', {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      grant_type: 'refresh_token',
      client_id: clientRegistration.client_id,
      refresh_token: tokens.refresh_token,
      resource: new URL('/mcp', fixture.origin).href,
    }),
  });
  assert.equal(refresh.status, 200);
  const refreshedTokens = (await refresh.json()) as {
    access_token: string;
    refresh_token: string;
  };
  assert.notEqual(refreshedTokens.refresh_token, tokens.refresh_token);

  const client = new Client(
    { name: 'agentk-docs-oauth-test', version: '0.0.0' },
    { versionNegotiation: { mode: 'auto' } },
  );
  const transport = new StreamableHTTPClientTransport(
    new URL('/mcp', fixture.origin),
    {
      authProvider: { token: async () => refreshedTokens.access_token },
    },
  );
  await client.connect(transport);
  const tools = await client.listTools();
  assert.deepEqual(
    tools.tools.map((tool) => tool.name),
    ['search_docs', 'lookup_api', 'read_doc'],
  );
  const search = await client.callTool({
    name: 'search_docs',
    arguments: { query: 'Session 和 Profile 区别', limit: 3 },
  });
  assert.equal(search.isError, undefined);
  const hits = (search.structuredContent as { hits: Array<{ url: string }> })
    .hits;
  const docsPrefix = new URL('/docs/', fixture.origin).href;
  assert.ok(hits.length > 0);
  assert.ok(hits.every((hit) => hit.url.startsWith(docsPrefix)));
  const page = await client.callTool({
    name: 'read_doc',
    arguments: { url: hits[0]!.url },
  });
  assert.equal(page.isError, undefined);
  await client.close();

  const legacyClient = new Client(
    { name: 'agentk-docs-oauth-legacy-test', version: '0.0.0' },
    { versionNegotiation: { mode: 'legacy' } },
  );
  await legacyClient.connect(
    new StreamableHTTPClientTransport(new URL('/mcp', fixture.origin), {
      authProvider: { token: async () => refreshedTokens.access_token },
    }),
  );
  assert.equal(legacyClient.getProtocolEra(), 'legacy');
  assert.equal((await legacyClient.listTools()).tools.length, 3);
  await legacyClient.close();

  const direct = await request(fixture.origin, '/mcp', {
    method: 'POST',
    headers: {
      authorization: `Bearer ${refreshedTokens.access_token}`,
      'content-type': 'application/json',
      accept: 'application/json, text/event-stream',
    },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'ping' }),
  });
  assert.equal(direct.headers.get('x-accel-buffering'), 'no');

  const revocation = await request(fixture.origin, '/oauth/revoke', {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      client_id: clientRegistration.client_id,
      token: refreshedTokens.access_token,
      token_type_hint: 'access_token',
    }),
  });
  assert.equal(revocation.status, 200);
  const revoked = await request(fixture.origin, '/mcp', {
    method: 'POST',
    headers: {
      authorization: `Bearer ${refreshedTokens.access_token}`,
      'content-type': 'application/json',
    },
    body: '{}',
  });
  assert.equal(revoked.status, 401);

  const refreshBody = (refreshToken: string) =>
    new URLSearchParams({
      grant_type: 'refresh_token',
      client_id: clientRegistration.client_id,
      refresh_token: refreshToken,
      resource: new URL('/mcp', fixture.origin).href,
    });
  const reusedRefresh = await request(fixture.origin, '/oauth/token', {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: refreshBody(tokens.refresh_token),
  });
  assert.equal(reusedRefresh.status, 400);
  const afterReplay = await request(fixture.origin, '/oauth/token', {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: refreshBody(refreshedTokens.refresh_token),
  });
  assert.equal(afterReplay.status, 400);
});

test('health is local-ready and service restart invalidates registrations', async () => {
  const first = await startFixture();
  const registration = await registerClient(first.origin);
  await first.close();

  const second = await startFixture();
  try {
    const health = await fetch(new URL('/healthz', second.origin));
    assert.equal(health.status, 200);
    const token = await request(second.origin, '/oauth/token', {
      method: 'POST',
      headers: { 'content-type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({
        grant_type: 'refresh_token',
        client_id: registration.client_id,
        refresh_token: 'old-token',
      }),
    });
    assert.equal(token.status, 400);
    assert.equal(
      ((await token.json()) as { error: string }).error,
      'invalid_client',
    );
    const docsRedirect = await request(second.origin, '/docs', {
      redirect: 'manual',
    });
    assert.equal(docsRedirect.status, 308);
    assert.equal(docsRedirect.headers.get('location'), '/docs/');
    const staticHome = await request(second.origin, '/docs/');
    assert.equal(staticHome.status, 200);
    const workerPage = await request(second.origin, '/docs/worker/');
    assert.equal(workerPage.status, 200);
    const compressedHome = await request(second.origin, '/docs/', {
      headers: { 'accept-encoding': 'gzip' },
    });
    assert.equal(compressedHome.headers.get('content-encoding'), 'gzip');
    assert.equal((await request(second.origin, '/')).status, 404);
    const invalidCatalog = await request(
      second.origin,
      '/docs/_agentk/capabilities',
      {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: '{not-json',
      },
    );
    assert.equal(invalidCatalog.status, 400);
    assert.deepEqual(await invalidCatalog.json(), {
      error: 'Invalid capability list request',
    });
    const unknownAgentk = await request(second.origin, '/docs/_agentk/unknown', {
      method: 'POST',
    });
    assert.equal(unknownAgentk.status, 404);
    assert.equal(
      (
        await request(second.origin, '/_agentk/capabilities', {
          method: 'POST',
        })
      ).status,
      404,
    );
    for (const pathname of [
      '/authorize',
      '/token',
      '/register',
      '/revoke',
      '/oauth/unknown',
      '/.well-known/unknown',
      '/.well-known/oauth-authorization-server/extra',
      '/.well-known/oauth-protected-resource/mcp/extra',
      '/mcp/unknown',
    ]) {
      const missing = await request(second.origin, pathname, { method: 'POST' });
      assert.equal(missing.status, 404, pathname);
      assert.match(missing.headers.get('content-type') ?? '', /^text\/plain/u);
    }
  } finally {
    await second.close();
  }
});
