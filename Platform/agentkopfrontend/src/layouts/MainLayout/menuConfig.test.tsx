import { describe, expect, it } from 'vitest';
import { buildPermittedMenuItems, resolveGroupKey, resolveSelectedKey } from './menuConfig';

interface MenuItemLike {
  key?: string;
  children?: MenuItemLike[];
}

function asMenuItems(items: ReturnType<typeof buildPermittedMenuItems>): MenuItemLike[] {
  return (items ?? []).filter(Boolean) as unknown as MenuItemLike[];
}

function groupChildren(items: MenuItemLike[], key: string): string[] {
  return items.find((item) => item.key === key)?.children?.map((item) => String(item.key)) ?? [];
}

describe('buildPermittedMenuItems', () => {
  it('shows every frontend entry when the local route switch is enabled', () => {
    const items = asMenuItems(buildPermittedMenuItems(new Set(), false, true));

    expect(items.map((item) => item.key)).toEqual([
      '/',
      'agent-assets',
      '/agents/new',
      'asset-library',
      'evaluation',
      'system',
    ]);
    expect(groupChildren(items, 'evaluation')).toEqual([
      '/offline-run/datasets',
      '/offline-run/jobs',
      '/session-review',
    ]);
  });

  it('expands module permission to its visible child entries', () => {
    const items = asMenuItems(
      buildPermittedMenuItems(new Set(['/offline-run', '/session-review']), false),
    );

    expect(groupChildren(items, 'evaluation')).toEqual([
      '/offline-run/datasets',
      '/offline-run/jobs',
      '/session-review',
    ]);
  });

  it('honors granular child permissions within a module', () => {
    const items = asMenuItems(buildPermittedMenuItems(new Set(['/offline-run/jobs']), false));

    expect(groupChildren(items, 'evaluation')).toEqual(['/offline-run/jobs']);
  });

  it('shows the new-agent entry when mine permission is granted', () => {
    const items = asMenuItems(buildPermittedMenuItems(new Set(['/agents/mine']), false));

    expect(items.map((item) => item.key)).toContain('/agents/new');
    expect(groupChildren(items, 'agent-assets')).toEqual(['/agents/mine']);
  });
});

describe('menu selection', () => {
  it('maps detail pages to their owning navigation entry', () => {
    expect(resolveSelectedKey('/agent100/42')).toBe('/agent100');
    expect(resolveSelectedKey('/agents/official/chat/ses_1')).toBe('/agents/official');
    expect(resolveSelectedKey('/offline-run/jobs/100')).toBe('/offline-run/jobs');
    expect(resolveSelectedKey('/session-review/ses_1')).toBe('/session-review');
  });

  it('resolves the parent navigation group', () => {
    expect(resolveGroupKey('/session-review')).toBe('evaluation');
    expect(resolveGroupKey('/permission-control')).toBe('system');
    expect(resolveGroupKey('/unknown')).toBeUndefined();
  });
});
