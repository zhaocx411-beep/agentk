import { randomBytes } from 'node:crypto';

import type { Response } from 'express';
import {
  InvalidClientMetadataError,
  InvalidGrantError,
  InvalidRequestError,
  InvalidScopeError,
  InvalidTargetError,
  InvalidTokenError,
  type AuthorizationParams,
  type OAuthRegisteredClientsStore,
  type OAuthServerProvider,
} from '@modelcontextprotocol/server-legacy/auth';
import type {
  AuthInfo,
  OAuthClientInformationFull,
  OAuthTokenRevocationRequest,
  OAuthTokens,
} from '@modelcontextprotocol/server';

import type { ApUser } from './ap-identity.js';

export const DOCS_READ_SCOPE = 'agentk.docs.read';
const CODE_LIFETIME_MS = 2 * 60 * 1_000;
const ACCESS_LIFETIME_MS = 60 * 60 * 1_000;
const REFRESH_LIFETIME_MS = 30 * 24 * 60 * 60 * 1_000;

const ALLOWED_CALLBACKS = new Set([
  'cursor://anysphere.cursor-mcp/oauth/callback',
  'https://www.cursor.com/agents/mcp/oauth/callback',
  'https://claude.ai/api/mcp/auth_callback',
  'https://claude.com/api/mcp/auth_callback',
]);

type Clock = () => number;
type SecretGenerator = () => string;

type PendingAuthorization = {
  clientId: string;
  redirectUri: string;
  codeChallenge: string;
  scopes: string[];
  resource: string;
  username: string;
  expiresAt: number;
};

// familyId ties every token descended from one authorization code together so
// that a replayed refresh token can take the whole chain down with it.
type Grant = {
  clientId: string;
  scopes: string[];
  resource: string;
  username: string;
  familyId: string;
  expiresAt: number;
};

function isAllowedRedirectUri(value: string): boolean {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return false;
  }
  if (url.username !== '' || url.password !== '' || url.hash !== '') {
    return false;
  }
  if (
    url.protocol === 'http:' &&
    ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
  ) {
    return true;
  }
  return ALLOWED_CALLBACKS.has(url.href);
}

function assertOnlyReadScope(scopes: readonly string[] | undefined): string[] {
  const requested = scopes ?? [DOCS_READ_SCOPE];
  if (
    requested.length !== 1 ||
    requested[0] !== DOCS_READ_SCOPE
  ) {
    throw new InvalidScopeError(`Only ${DOCS_READ_SCOPE} is supported`);
  }
  return [DOCS_READ_SCOPE];
}

export class InMemoryOAuthProvider implements OAuthServerProvider {
  readonly #resource: URL;
  readonly #clock: Clock;
  readonly #secret: SecretGenerator;
  readonly #clients = new Map<string, OAuthClientInformationFull>();
  readonly #codes = new Map<string, PendingAuthorization>();
  readonly #accessTokens = new Map<string, Grant>();
  readonly #refreshTokens = new Map<string, Grant>();
  readonly #consumedRefreshTokens = new Map<
    string,
    { familyId: string; expiresAt: number }
  >();
  readonly #cleanupTimer: NodeJS.Timeout;

  readonly clientsStore: OAuthRegisteredClientsStore;

  constructor(options: {
    resource: URL;
    clock?: Clock;
    secret?: SecretGenerator;
  }) {
    this.#resource = options.resource;
    this.#clock = options.clock ?? Date.now;
    this.#secret = options.secret ?? (() => randomBytes(32).toString('base64url'));
    this.clientsStore = {
      getClient: (clientId) => this.#clients.get(clientId),
      registerClient: (client) => this.#registerClient(client),
    };
    this.#cleanupTimer = setInterval(() => this.#cleanup(), 60_000);
    this.#cleanupTimer.unref();
  }

  close(): void {
    clearInterval(this.#cleanupTimer);
  }

  #registerClient(
    input: Omit<OAuthClientInformationFull, 'client_id' | 'client_id_issued_at'>,
  ): OAuthClientInformationFull {
    if (
      input.token_endpoint_auth_method !== 'none' ||
      input.redirect_uris.length === 0 ||
      input.redirect_uris.some((uri) => !isAllowedRedirectUri(uri))
    ) {
      throw new InvalidClientMetadataError(
        'Only supported public-client redirect URIs are allowed',
      );
    }
    const grantTypes = input.grant_types ?? ['authorization_code'];
    if (
      !grantTypes.includes('authorization_code') ||
      grantTypes.some(
        (value) => !['authorization_code', 'refresh_token'].includes(value),
      )
    ) {
      throw new InvalidClientMetadataError(
        'Only authorization_code and refresh_token grants are supported',
      );
    }
    const responseTypes = input.response_types ?? ['code'];
    if (responseTypes.some((value) => value !== 'code')) {
      throw new InvalidClientMetadataError('Only the code response type is supported');
    }

    const client: OAuthClientInformationFull = {
      ...input,
      grant_types: [...new Set([...grantTypes, 'refresh_token'])],
      response_types: ['code'],
      client_id: this.#secret(),
      client_id_issued_at: Math.floor(this.#clock() / 1_000),
    };
    this.#clients.set(client.client_id, client);
    return client;
  }

  async authorize(
    client: OAuthClientInformationFull,
    params: AuthorizationParams,
    response: Response,
  ): Promise<void> {
    const user = response.locals.apUser as ApUser | undefined;
    if (!user) {
      throw new InvalidRequestError('Missing verified AP identity');
    }
    this.#assertResource(params.resource);
    const scopes = assertOnlyReadScope(params.scopes);
    const code = this.#secret();
    this.#codes.set(code, {
      clientId: client.client_id,
      redirectUri: params.redirectUri,
      codeChallenge: params.codeChallenge,
      scopes,
      resource: this.#resource.href,
      username: user.username,
      expiresAt: this.#clock() + CODE_LIFETIME_MS,
    });
    const redirect = new URL(params.redirectUri);
    redirect.searchParams.set('code', code);
    if (params.state !== undefined) {
      redirect.searchParams.set('state', params.state);
    }
    response.redirect(302, redirect.href);
  }

  async challengeForAuthorizationCode(
    client: OAuthClientInformationFull,
    authorizationCode: string,
  ): Promise<string> {
    const grant = this.#liveCode(authorizationCode);
    if (grant.clientId !== client.client_id) {
      throw new InvalidGrantError('Authorization code client mismatch');
    }
    return grant.codeChallenge;
  }

  async exchangeAuthorizationCode(
    client: OAuthClientInformationFull,
    authorizationCode: string,
    _codeVerifier?: string,
    redirectUri?: string,
    resource?: URL,
  ): Promise<OAuthTokens> {
    const grant = this.#liveCode(authorizationCode);
    this.#codes.delete(authorizationCode);
    if (
      grant.clientId !== client.client_id ||
      grant.redirectUri !== redirectUri ||
      !this.#resourceMatches(resource, grant.resource)
    ) {
      throw new InvalidGrantError('Authorization code binding mismatch');
    }
    return this.#issueTokens({ ...grant, familyId: this.#secret() });
  }

  async exchangeRefreshToken(
    client: OAuthClientInformationFull,
    refreshToken: string,
    scopes?: string[],
    resource?: URL,
  ): Promise<OAuthTokens> {
    const consumed = this.#consumedRefreshTokens.get(refreshToken);
    if (consumed !== undefined) {
      this.#revokeFamily(consumed.familyId);
      throw new InvalidGrantError('Refresh token has already been used');
    }
    const grant = this.#refreshTokens.get(refreshToken);
    if (!grant || grant.expiresAt <= this.#clock()) {
      this.#refreshTokens.delete(refreshToken);
      throw new InvalidGrantError('Unknown or expired refresh token');
    }
    this.#refreshTokens.delete(refreshToken);
    this.#consumedRefreshTokens.set(refreshToken, {
      familyId: grant.familyId,
      expiresAt: grant.expiresAt,
    });
    if (
      grant.clientId !== client.client_id ||
      !this.#resourceMatches(resource, grant.resource)
    ) {
      throw new InvalidGrantError('Refresh token binding mismatch');
    }
    const requestedScopes = scopes ?? grant.scopes;
    if (requestedScopes.some((scope) => !grant.scopes.includes(scope))) {
      throw new InvalidScopeError('Requested scope exceeds the original grant');
    }
    return this.#issueTokens({ ...grant, scopes: requestedScopes });
  }

  async verifyAccessToken(token: string): Promise<AuthInfo> {
    const grant = this.#accessTokens.get(token);
    if (!grant || grant.expiresAt <= this.#clock()) {
      this.#accessTokens.delete(token);
      throw new InvalidTokenError('Unknown or expired access token');
    }
    return {
      token,
      clientId: grant.clientId,
      scopes: grant.scopes,
      expiresAt: Math.floor(grant.expiresAt / 1_000),
      resource: new URL(grant.resource),
      extra: { username: grant.username },
    };
  }

  async revokeToken(
    client: OAuthClientInformationFull,
    request: OAuthTokenRevocationRequest,
  ): Promise<void> {
    const access = this.#accessTokens.get(request.token);
    if (access?.clientId === client.client_id) {
      this.#accessTokens.delete(request.token);
    }
    const refresh = this.#refreshTokens.get(request.token);
    if (refresh?.clientId === client.client_id) {
      this.#refreshTokens.delete(request.token);
    }
  }

  #liveCode(code: string): PendingAuthorization {
    const grant = this.#codes.get(code);
    if (!grant || grant.expiresAt <= this.#clock()) {
      this.#codes.delete(code);
      throw new InvalidGrantError('Unknown or expired authorization code');
    }
    return grant;
  }

  #assertResource(resource: URL | undefined): void {
    if (resource?.href !== this.#resource.href) {
      throw new InvalidTargetError(`resource must equal ${this.#resource.href}`);
    }
  }

  #resourceMatches(resource: URL | undefined, expected: string): boolean {
    return resource?.href === expected && expected === this.#resource.href;
  }

  #issueTokens(grant: Omit<Grant, 'expiresAt'>): OAuthTokens {
    const now = this.#clock();
    const accessToken = this.#secret();
    const refreshToken = this.#secret();
    const issued = {
      clientId: grant.clientId,
      scopes: grant.scopes,
      resource: grant.resource,
      username: grant.username,
      familyId: grant.familyId,
    };
    this.#accessTokens.set(accessToken, {
      ...issued,
      expiresAt: now + ACCESS_LIFETIME_MS,
    });
    this.#refreshTokens.set(refreshToken, {
      ...issued,
      expiresAt: now + REFRESH_LIFETIME_MS,
    });
    return {
      access_token: accessToken,
      token_type: 'Bearer',
      expires_in: Math.floor(ACCESS_LIFETIME_MS / 1_000),
      refresh_token: refreshToken,
      scope: grant.scopes.join(' '),
    };
  }

  #revokeFamily(familyId: string): void {
    for (const [token, grant] of this.#accessTokens) {
      if (grant.familyId === familyId) this.#accessTokens.delete(token);
    }
    for (const [token, grant] of this.#refreshTokens) {
      if (grant.familyId === familyId) this.#refreshTokens.delete(token);
    }
  }

  #cleanup(): void {
    const now = this.#clock();
    for (const [key, grant] of this.#codes) {
      if (grant.expiresAt <= now) this.#codes.delete(key);
    }
    for (const [key, grant] of this.#accessTokens) {
      if (grant.expiresAt <= now) this.#accessTokens.delete(key);
    }
    for (const [key, grant] of this.#refreshTokens) {
      if (grant.expiresAt <= now) this.#refreshTokens.delete(key);
    }
    for (const [key, consumed] of this.#consumedRefreshTokens) {
      if (consumed.expiresAt <= now) this.#consumedRefreshTokens.delete(key);
    }
  }
}
