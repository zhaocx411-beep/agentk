import assert from 'node:assert/strict';
import test from 'node:test';

import {
  openApIdentityVerifier,
  type KsapIdentityClient,
} from './ap-identity.js';

test('AP verifier enables issuer and host validation and normalizes the peer IP', async () => {
  let initialized: Parameters<KsapIdentityClient['init']>[0] | undefined;
  let verification: [string, string] | undefined;
  const client: KsapIdentityClient = {
    async init(options) {
      initialized = options;
    },
    verifyToken(token, ip) {
      verification = [token, ip];
      return {
        result: 1,
        message: 'ok',
        data: { username: 'alice', name: 'Alice' },
      };
    },
  };
  const verifier = await openApIdentityVerifier(
    { jwksUrl: 'https://ksap.test/jwks', expectedHost: 'docs.test' },
    client,
  );
  assert.deepEqual(initialized, {
    jwksUrl: 'https://ksap.test/jwks',
    verifyIss: true,
    trustedHosts: ['docs.test'],
    debug: false,
    fileLogConfig: { isOpen: false },
  });
  assert.deepEqual(await verifier.verify('token', '::ffff:10.1.2.3'), {
    username: 'alice',
    displayName: 'Alice',
  });
  assert.deepEqual(verification, ['token', '10.1.2.3']);
});

test('AP verifier rejects missing and SDK-rejected identities', async () => {
  const client: KsapIdentityClient = {
    async init() {
    },
    verifyToken() {
      return { result: 0, message: 'expired or invalid', data: undefined };
    },
  };
  const verifier = await openApIdentityVerifier(
    { jwksUrl: 'https://ksap.test/jwks', expectedHost: 'docs.test' },
    client,
  );
  await assert.rejects(verifier.verify(undefined, '10.1.2.3'), /Missing AP identity/);
  await assert.rejects(verifier.verify('bad', '10.1.2.3'), /Invalid AP identity/);
});
