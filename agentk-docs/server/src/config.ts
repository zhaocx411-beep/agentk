import path from 'node:path';

import { REPOSITORY_ROOT } from './paths.js';

export type DocsServiceConfig = {
  publicUrl: URL;
  publicOrigin: URL;
  mcpResource: URL;
  kwsServiceName: string;
  kwsServiceCredential?: string;
  ksapJwksUrl: string;
  repositoryRoot: string;
  buildRoot: string;
  host: string;
  port: number;
};

function requiredEnvironment(
  environment: NodeJS.ProcessEnv,
  name: string,
): string {
  const value = environment[name]?.trim();
  if (!value) {
    throw new Error(`Missing required environment variable: ${name}`);
  }
  return value;
}

function requiredOpaqueEnvironment(
  environment: NodeJS.ProcessEnv,
  name: string,
): string {
  const value = environment[name];
  if (value === undefined || value === '') {
    throw new Error(`Missing required environment variable: ${name}`);
  }
  return value;
}

function optionalOpaqueEnvironment(
  environment: NodeJS.ProcessEnv,
  name: string,
): string | undefined {
  const value = environment[name];
  return value === undefined || value === '' ? undefined : value;
}

function parsePublicUrl(value: string): URL {
  const url = new URL(value);
  if (
    url.protocol !== 'https:' ||
    url.username !== '' ||
    url.password !== '' ||
    url.pathname !== '/docs/' ||
    url.search !== '' ||
    url.hash !== ''
  ) {
    throw new Error(
      'AGENTK_DOCS_PUBLIC_URL must be an HTTPS URL with the exact path /docs/',
    );
  }
  return url;
}

function parsePort(value: string | undefined): number {
  if (value === undefined || value.trim() === '') {
    return 8080;
  }
  const port = Number(value);
  if (!Number.isInteger(port) || port < 1 || port > 65_535) {
    throw new Error('AUTO_PORT0 or PORT must be an integer from 1 to 65535');
  }
  return port;
}

export function loadPublicUrl(
  environment: NodeJS.ProcessEnv = process.env,
): URL {
  return parsePublicUrl(
    requiredEnvironment(environment, 'AGENTK_DOCS_PUBLIC_URL'),
  );
}

export function loadDocsServiceConfig(
  environment: NodeJS.ProcessEnv = process.env,
): DocsServiceConfig {
  const publicUrl = loadPublicUrl(environment);
  const publicOrigin = new URL(publicUrl.origin);

  const repositoryRoot = REPOSITORY_ROOT;
  const kwsServiceCredential = optionalOpaqueEnvironment(
    environment,
    'KWS_SERVICE_CREDENTIAL',
  );
  return {
    publicUrl,
    publicOrigin,
    mcpResource: new URL('/mcp', publicOrigin),
    kwsServiceName: requiredOpaqueEnvironment(environment, 'KWS_SERVICE_NAME'),
    ...(kwsServiceCredential === undefined
      ? {}
      : { kwsServiceCredential }),
    ksapJwksUrl: requiredEnvironment(environment, 'KSAP_JWKS_URL'),
    repositoryRoot,
    buildRoot: path.join(repositoryRoot, 'build'),
    host: '0.0.0.0',
    port: parsePort(environment.AUTO_PORT0 ?? environment.PORT),
  };
}
