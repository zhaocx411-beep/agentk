import assert from 'node:assert/strict';
import test from 'node:test';

import type { Response } from 'express';
import type { OAuthClientInformationFull } from '@modelcontextprotocol/server';

import { DOCS_READ_SCOPE, InMemoryOAuthProvider } from './auth.js';

function clientInput(
  overrides: Record<string, unknown> = {},
): Omit<OAuthClientInformationFull, 'client_id' | 'client_id_issued_at'> {
  return {
    redirect_uris: ['http://127.0.0.1:54321/callback'],
    token_endpoint_auth_method: 'none',
    grant_types: ['authorization_code', 'refresh_token'],
    response_types: ['code'],
    client_name: 'AgentK Docs test',
    ...overrides,
  } as Omit<OAuthClientInformationFull, 'client_id' | 'client_id_issued_at'>;
}

test('DCR accepts only supported public clients and callbacks', async () => {
  const provider = new InMemoryOAuthProvider({
    resource: new URL('https://docs.test/mcp'),
  });
  try {
    const valid = await provider.clientsStore.registerClient?.(clientInput());
    assert.equal(valid?.token_endpoint_auth_method, 'none');
    assert.deepEqual(valid?.grant_types, ['authorization_code', 'refresh_token']);
    for (const redirectUri of [
      'cursor://anysphere.cursor-mcp/oauth/callback',
      'https://www.cursor.com/agents/mcp/oauth/callback',
      'https://claude.ai/api/mcp/auth_callback',
      'https://claude.com/api/mcp/auth_callback',
      'http://[::1]:62000/callback',
    ]) {
      assert.ok(
        await provider.clientsStore.registerClient?.(
          clientInput({ redirect_uris: [redirectUri] }),
        ),
      );
    }

    // Cursor desktop registers its whole callback set in one request.
    assert.ok(
      await provider.clientsStore.registerClient?.(
        clientInput({
          redirect_uris: [
            'cursor://anysphere.cursor-mcp/oauth/callback',
            'https://www.cursor.com/agents/mcp/oauth/callback',
            'http://localhost:8787/callback',
          ],
        }),
      ),
    );

    await assert.rejects(
      async () =>
        provider.clientsStore.registerClient?.(
          clientInput({ redirect_uris: ['https://attacker.example/callback'] }),
        ),
      /public-client redirect URIs/,
    );
    await assert.rejects(
      async () =>
        provider.clientsStore.registerClient?.(
          clientInput({ token_endpoint_auth_method: 'client_secret_post' }),
        ),
      /public-client redirect URIs/,
    );
  } finally {
    provider.close();
  }
});

test('authorization code is single-use and refresh tokens rotate', async () => {
  let now = 1_000_000;
  let sequence = 0;
  const provider = new InMemoryOAuthProvider({
    resource: new URL('https://docs.test/mcp'),
    clock: () => now,
    secret: () => `secret-${++sequence}`,
  });
  try {
    const client = await provider.clientsStore.registerClient?.(clientInput());
    assert.ok(client);
    let location = '';
    const response = {
      locals: { apUser: { username: 'alice', displayName: 'Alice' } },
      redirect: (_status: number, value: string) => {
        location = value;
      },
    } as unknown as Response;

    await provider.authorize(
      client,
      {
        state: 'state-1',
        scopes: [DOCS_READ_SCOPE],
        codeChallenge: 'challenge',
        redirectUri: client.redirect_uris[0]!,
        resource: new URL('https://docs.test/mcp'),
      },
      response,
    );
    const redirect = new URL(location);
    const code = redirect.searchParams.get('code');
    assert.ok(code);
    assert.equal(redirect.searchParams.get('state'), 'state-1');
    assert.equal(
      await provider.challengeForAuthorizationCode(client, code),
      'challenge',
    );

    const tokens = await provider.exchangeAuthorizationCode(
      client,
      code,
      undefined,
      client.redirect_uris[0],
      new URL('https://docs.test/mcp'),
    );
    assert.equal(tokens.scope, DOCS_READ_SCOPE);
    const auth = await provider.verifyAccessToken(tokens.access_token);
    assert.equal(auth.extra?.username, 'alice');
    assert.equal(auth.resource?.href, 'https://docs.test/mcp');
    await assert.rejects(
      provider.exchangeAuthorizationCode(
        client,
        code,
        undefined,
        client.redirect_uris[0],
        new URL('https://docs.test/mcp'),
      ),
      /Unknown or expired authorization code/,
    );

    assert.ok(tokens.refresh_token);
    const rotated = await provider.exchangeRefreshToken(
      client,
      tokens.refresh_token,
      [DOCS_READ_SCOPE],
      new URL('https://docs.test/mcp'),
    );
    assert.notEqual(rotated.refresh_token, tokens.refresh_token);

    now += 60 * 60 * 1_000 + 1;
    await assert.rejects(
      provider.verifyAccessToken(tokens.access_token),
      /expired access token/,
    );
    now += 30 * 24 * 60 * 60 * 1_000;
    await assert.rejects(
      provider.exchangeRefreshToken(
        client,
        rotated.refresh_token!,
        [DOCS_READ_SCOPE],
        new URL('https://docs.test/mcp'),
      ),
      /expired refresh token/,
    );
    await assert.rejects(
      provider.exchangeRefreshToken(
        client,
        tokens.refresh_token,
        [DOCS_READ_SCOPE],
        new URL('https://docs.test/mcp'),
      ),
      /already been used/,
    );
  } finally {
    provider.close();
  }
});

test('replaying a rotated refresh token revokes the whole family', async () => {
  let sequence = 0;
  const provider = new InMemoryOAuthProvider({
    resource: new URL('https://docs.test/mcp'),
    secret: () => `secret-${++sequence}`,
  });
  const resource = new URL('https://docs.test/mcp');
  try {
    const client = await provider.clientsStore.registerClient?.(clientInput());
    assert.ok(client);
    let location = '';
    await provider.authorize(
      client,
      {
        scopes: [DOCS_READ_SCOPE],
        codeChallenge: 'challenge',
        redirectUri: client.redirect_uris[0]!,
        resource,
      },
      {
        locals: { apUser: { username: 'alice' } },
        redirect: (_status: number, value: string) => {
          location = value;
        },
      } as unknown as Response,
    );
    const code = new URL(location).searchParams.get('code');
    assert.ok(code);
    const tokens = await provider.exchangeAuthorizationCode(
      client,
      code,
      undefined,
      client.redirect_uris[0],
      resource,
    );
    assert.ok(tokens.refresh_token);
    const rotated = await provider.exchangeRefreshToken(
      client,
      tokens.refresh_token,
      [DOCS_READ_SCOPE],
      resource,
    );
    assert.ok(rotated.refresh_token);
    assert.equal(
      (await provider.verifyAccessToken(rotated.access_token)).extra?.username,
      'alice',
    );

    await assert.rejects(
      provider.exchangeRefreshToken(
        client,
        tokens.refresh_token,
        [DOCS_READ_SCOPE],
        resource,
      ),
      /already been used/,
    );
    await assert.rejects(
      provider.verifyAccessToken(rotated.access_token),
      /Unknown or expired access token/,
    );
    await assert.rejects(
      provider.exchangeRefreshToken(
        client,
        rotated.refresh_token,
        [DOCS_READ_SCOPE],
        resource,
      ),
      /Unknown or expired refresh token/,
    );
  } finally {
    provider.close();
  }
});

test('scope and resource are exact', async () => {
  const provider = new InMemoryOAuthProvider({
    resource: new URL('https://docs.test/mcp'),
  });
  try {
    const client = await provider.clientsStore.registerClient?.(clientInput());
    assert.ok(client);
    const response = {
      locals: { apUser: { username: 'alice' } },
      redirect: () => undefined,
    } as unknown as Response;
    await assert.rejects(
      provider.authorize(
        client,
        {
          scopes: ['other'],
          codeChallenge: 'challenge',
          redirectUri: client.redirect_uris[0]!,
          resource: new URL('https://docs.test/mcp'),
        },
        response,
      ),
      /Only agentk.docs.read/,
    );
    await assert.rejects(
      provider.authorize(
        client,
        {
          scopes: [DOCS_READ_SCOPE],
          codeChallenge: 'challenge',
          redirectUri: client.redirect_uris[0]!,
          resource: new URL('https://docs.test/other'),
        },
        response,
      ),
      /resource must equal/,
    );
  } finally {
    provider.close();
  }
});
