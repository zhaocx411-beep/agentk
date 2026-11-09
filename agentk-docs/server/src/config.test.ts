import assert from 'node:assert/strict';
import test from 'node:test';

import { loadDocsServiceConfig } from './config.js';

const validEnvironment = {
  AGENTK_DOCS_PUBLIC_URL: 'https://agentk.test/docs/',
  KWS_SERVICE_NAME: 'klingai-agentk-docs',
  KWS_SERVICE_CREDENTIAL: 'credential',
  KSAP_JWKS_URL: 'https://ksap.test/jwks',
};

test('production config requires identity, KSN, and public URL', () => {
  const config = loadDocsServiceConfig(validEnvironment);
  assert.equal(config.publicUrl.href, 'https://agentk.test/docs/');
  assert.equal(config.publicOrigin.href, 'https://agentk.test/');
  assert.equal(config.mcpResource.href, 'https://agentk.test/mcp');
  assert.equal(config.port, 8080);
  for (const name of [
    'AGENTK_DOCS_PUBLIC_URL',
    'KWS_SERVICE_NAME',
    'KSAP_JWKS_URL',
  ]) {
    const environment = { ...validEnvironment } as Record<string, string | undefined>;
    delete environment[name];
    assert.throws(
      () => loadDocsServiceConfig(environment),
      new RegExp(`Missing required environment variable: ${name}`),
    );
  }
});

test('KSN is byte-preserving and credential is optional', () => {
  const withoutCredential = loadDocsServiceConfig({
    ...validEnvironment,
    KWS_SERVICE_NAME: ' AgentK/原样 ',
    KWS_SERVICE_CREDENTIAL: '',
  });
  assert.equal(withoutCredential.kwsServiceName, ' AgentK/原样 ');
  assert.equal(withoutCredential.kwsServiceCredential, undefined);

  const withCredential = loadDocsServiceConfig({
    ...validEnvironment,
    KWS_SERVICE_CREDENTIAL: ' credential原样 ',
  });
  assert.equal(withCredential.kwsServiceCredential, ' credential原样 ');
});

test('production public URL is HTTPS with the exact /docs/ path', () => {
  for (const value of [
    'http://agentk.test/docs/',
    'https://agentk.test',
    'https://agentk.test/docs',
    'https://agentk.test/other/',
    'https://agentk.test/docs/?query=1',
    'https://agentk.test/docs/#fragment',
    'https://user:pass@agentk.test/docs/',
  ]) {
    assert.throws(() =>
      loadDocsServiceConfig({
        ...validEnvironment,
        AGENTK_DOCS_PUBLIC_URL: value,
      }),
    );
  }
});
