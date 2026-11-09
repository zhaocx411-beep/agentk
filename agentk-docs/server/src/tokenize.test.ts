import assert from 'node:assert/strict';
import test from 'node:test';

import {identifierTokens, normalizeText, tokenize} from './tokenize.js';

test('normalizes Unicode and English case', () => {
  assert.equal(normalizeText('ＡgentK'), 'agentk');
});

test('segments Chinese and mixed-language queries', () => {
  const tokens = tokenize('Session 和 Profile 区别');
  assert.ok(tokens.includes('session'));
  assert.ok(tokens.includes('profile'));
  assert.ok(tokens.includes('区别'));
});

test('preserves and splits qualified and styled identifiers', () => {
  assert.deepEqual(
    new Set(identifierTokens('agentk.worker.invoke_capability')),
    new Set([
      'agentk.worker.invoke_capability',
      'agentk',
      'worker',
      'invoke',
      'capability',
    ]),
  );
  const tokens = new Set(
    identifierTokens('CreateSession startExecution HTTPClient storyboard-job'),
  );
  for (const token of [
    'createsession',
    'create',
    'session',
    'startexecution',
    'start',
    'execution',
    'httpclient',
    'http',
    'client',
    'storyboard-job',
    'storyboard',
    'job',
  ]) {
    assert.ok(tokens.has(token), `missing ${token}`);
  }
});
