import { describe, expect, it } from 'vitest';
import {
  evidenceURL,
  completedPlanEvidence,
  pageSuggestions,
  managementURL,
  navigationURL,
  nextWriteStep,
  type Plan,
  planState,
} from './state';

const plan: Plan = {
  id: 'plan-1',
  intent: '停用一个用户',
  maxRisk: 'medium',
  affected: 1,
  mode: 'all',
  status: 'previewed',
  steps: [
    {
      seq: 0,
      capability: 'system.user.list',
      args: {},
      note: '查询用户',
      status: 'previewed',
      result: [],
    },
    {
      seq: 1,
      capability: 'system.user.changeStatus',
      args: { userId: 2, status: '1' },
      note: '停用',
      status: 'previewed',
      preview: {
        summary: '停用用户',
        affected: 1,
        skipped: 0,
        reversible: true,
      },
    },
  ],
};
describe('plan confirmation state', () => {
  it('confirms the next write sequence, skipping preceding read steps', () => {
    expect(nextWriteStep(plan)?.seq).toBe(1);
    expect(planState(plan, 'high').executable).toBe(true);
  });
  it('keeps critical plans manual even for a critical ceiling', () => {
    expect(
      planState({ ...plan, maxRisk: 'critical' }, 'critical').executable,
    ).toBe(false);
  });
  it('requires fresh preview after expiry and respects a lower user ceiling', () => {
    expect(
      planState({ ...plan, expiresAt: '2020-01-01T00:00:00Z' }, 'high').expired,
    ).toBe(true);
    expect(planState(plan, 'low').executable).toBe(false);
  });
  it('requires stepwise confirmation for steps depending on a write result', () => {
    const pending = {
      ...plan,
      steps: [plan.steps[1], { ...plan.steps[1], seq: 2, status: 'pending' }],
    };
    expect(planState(pending, 'high').requiresStepwise).toBe(true);
  });
});
describe('evidence links', () => {
  it('only navigates to authorized local pages, retaining list filters', () => {
    const menus = [
      {
        path: '/system',
        component: 'Layout',
        children: [
          { path: 'user', component: 'system/user/index' },
          {
            path: 'ai-provider',
            component: 'system/ai-provider/index',
            hidden: true,
          },
          {
            path: 'https://example.com',
            component: 'external',
            external: true,
          },
        ],
      },
    ];
    expect(
      navigationURL({ route: '/system/user', filters: { status: '1' } }, menus),
    ).toBe('/system/user?status=1');
    expect(navigationURL({ route: '/system/ai-provider' }, menus)).toBe(
      '/system/ai-provider',
    );
    for (const route of [
      '/system',
      '/system/role',
      'https://example.com',
      '//example.com',
      '/login',
      '/system/user/../../admin',
    ]) {
      expect(navigationURL({ route }, menus)).toBeUndefined();
    }
    expect(navigationURL({ route: '/system/user' }, [])).toBeUndefined();
  });
  it('preserves route query and safely encodes filters', () => {
    const url = evidenceURL({
      route: '/system/user?status=0',
      filters: { status: '1', userName: 'a&b', ids: [2, 3] },
    });
    if (!url) throw new Error('expected a local evidence URL');
    const parsed = new URL(url, 'https://intent.invalid');
    expect(parsed.searchParams.get('userName')).toBe('a&b');
    expect(parsed.searchParams.get('status')).toBe('1');
    expect(parsed.searchParams.get('ids')).toBe('2,3');
  });
  it('rejects external and protocol-relative routes', () => {
    expect(evidenceURL({ route: 'https://evil.invalid' })).toBeUndefined();
    expect(evidenceURL({ route: '//evil.invalid' })).toBeUndefined();
  });
  it('uses the bundled history route for changeset capabilities', () => {
    expect(
      managementURL({
        ...plan.steps[1],
        capability: 'system.changeset.rollback',
      }),
    ).toBe('/changesets');
  });
});

describe('optional follow-up pages', () => {
  const menus = [
    {
      path: '/system/user',
      component: 'system/user/index',
      meta: { title: '用户管理' },
    },
  ];
  it('only suggests successful steps after the entire plan completes', () => {
    expect(completedPlanEvidence(plan)).toEqual([]);
    expect(completedPlanEvidence({ ...plan, status: 'partial' })).toEqual([]);
    expect(completedPlanEvidence({ ...plan, status: 'aborted' })).toEqual([]);
    const finished = {
      ...plan,
      status: 'done',
      steps: [
        { ...plan.steps[1], status: 'applied' },
        {
          ...plan.steps[1],
          status: 'failed',
          capability: 'system.role.update',
        },
        {
          ...plan.steps[1],
          status: 'skipped',
          capability: 'system.menu.update',
        },
      ],
    };
    expect(completedPlanEvidence(finished)).toEqual([
      { route: '/system/user' },
    ]);
  });
  it('keeps authorized titles and filters, deduplicates entries and hides revoked pages', () => {
    const evidence = [
      { route: '/system/user', filters: { status: '1' } },
      { route: '/system/user?status=1' },
      { route: '/system/role' },
      { route: 'https://example.com' },
    ];
    expect(pageSuggestions(evidence, menus)).toEqual([
      { url: '/system/user?status=1', title: '用户管理' },
    ]);
    expect(pageSuggestions(evidence, [])).toEqual([]);
  });
});
