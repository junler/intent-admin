import { describe, expect, it } from 'vitest';

import { hasPermission } from './permission';

describe('hasPermission', () => {
  it('requires an explicit grant for each permission and supports any/all checks', () => {
    const grants = ['demo:item:list', 'demo:item:edit'];
    expect(hasPermission(grants, 'demo:item:list')).toBe(true);
    expect(hasPermission(grants, 'demo:item:remove')).toBe(false);
    expect(hasPermission(grants, ['demo:item:edit', 'demo:item:remove'])).toBe(
      true,
    );
    expect(
      hasPermission(grants, ['demo:item:edit', 'demo:item:remove'], 'all'),
    ).toBe(false);
    expect(
      hasPermission(grants, ['demo:item:edit', 'demo:item:list'], 'all'),
    ).toBe(true);
  });

  it('allows the superadmin wildcard without treating partial wildcards as grants', () => {
    expect(
      hasPermission(['*:*:*'], ['demo:item:remove', 'system:user:edit'], 'all'),
    ).toBe(true);
    expect(hasPermission(['demo:item:*'], 'demo:item:edit')).toBe(false);
  });

  it('allows an empty permission requirement for any authenticated user', () => {
    expect(hasPermission([], '')).toBe(true);
    expect(hasPermission([], [])).toBe(true);
    expect(hasPermission([], [''])).toBe(true);
  });
});
