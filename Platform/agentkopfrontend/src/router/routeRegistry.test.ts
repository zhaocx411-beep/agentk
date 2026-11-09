import { describe, expect, it } from 'vitest';
import {
  canAccessRoute,
  collectAllowedMenuUrls,
  matchesMenuUrlPattern,
  resolveCanonicalMenuUrl,
} from './routeRegistry';

describe('route permission matching', () => {
  it('matches dynamic paths without accepting extra segments', () => {
    expect(matchesMenuUrlPattern('/agents/123', '/agents/:agentId')).toBe(true);
    expect(matchesMenuUrlPattern('/agents/123/edit', '/agents/:agentId')).toBe(false);
  });

  it('chooses the static official route over a dynamic agent route', () => {
    expect(resolveCanonicalMenuUrl('/agents/official')).toBe('/agents/official');
    expect(resolveCanonicalMenuUrl('/agents/42')).toBe('/agents/:agentId');
    expect(resolveCanonicalMenuUrl('/session-review/ses_1')).toBe(
      '/session-review/:agentkSessionId',
    );
  });

  it('collects nested backend permissions', () => {
    const allowed = collectAllowedMenuUrls([
      {
        id: 1,
        menuName: 'Agent',
        menuUrl: '/agents',
        children: [
          {
            id: 2,
            menuName: '我的 Agent',
            menuUrl: '/agents/mine',
          },
        ],
      },
    ]);

    expect(allowed).toEqual(new Set(['/agents', '/agents/mine']));
  });

  it('uses granular child permissions once a module provides them', () => {
    const allowed = new Set(['/agents', '/agents/mine']);

    expect(canAccessRoute('/agents/mine', allowed, false)).toBe(true);
    expect(canAccessRoute('/agents/official', allowed, false)).toBe(false);
    expect(canAccessRoute('/agents/new', allowed, false)).toBe(true);
  });

  it('always allows super administrators', () => {
    expect(canAccessRoute('/permission-control', new Set(), true)).toBe(true);
  });
});
