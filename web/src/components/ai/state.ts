import type { Evidence, Preview, Risk } from '@/api/types';
import {
  flattenPages,
  normalizeMenus,
  safeRedirect,
  type MenuRoute,
} from '@/router/menu';

export interface PlanStep {
  seq: number;
  capability: string;
  args: unknown;
  note: string;
  preview?: Preview;
  result?: unknown;
  status: string;
  error?: string;
}
export interface Plan {
  id: string;
  intent: string;
  steps: PlanStep[];
  maxRisk: Risk;
  affected: number;
  mode: string;
  status: string;
  expiresAt?: string;
}
const levels: Record<Risk, number> = {
  low: 0,
  medium: 1,
  high: 2,
  critical: 3,
};

export function evidenceURL(evidence: Evidence): string | undefined {
  const route = safeRedirect(evidence.route, '');
  if (!route) return undefined;
  const url = new URL(route, 'https://intent.invalid');
  for (const [key, value] of Object.entries(evidence.filters ?? {})) {
    if (value === undefined || value === null || value === '') continue;
    url.searchParams.set(
      key,
      typeof value === 'object' && !Array.isArray(value)
        ? JSON.stringify(value)
        : String(value),
    );
  }
  return `${url.pathname}${url.search}${url.hash}`;
}

/** Model-provided navigation must resolve to a currently authorized local page. */
export function navigationURL(
  evidence: Evidence,
  menus: MenuRoute[],
): string | undefined {
  const url = evidenceURL(evidence);
  if (!url) return undefined;
  const pathname = new URL(url, 'https://intent.invalid').pathname;
  return flattenPages(normalizeMenus(menus)).some(
    (page) => page.path === pathname,
  )
    ? url
    : undefined;
}

export function nextWriteStep(plan: Plan): PlanStep | undefined {
  // Read steps have no Preview. Their zero-based seq must never be sent as
  // the stepwise write confirmation when a later write is the next action.
  return plan.steps.find(
    (step) => step.preview && !['applied', 'skipped'].includes(step.status),
  );
}

export function planState(
  plan: Plan,
  allowed: Risk | undefined,
  now = Date.now(),
) {
  const exceeded =
    plan.maxRisk === 'critical' ||
    (levels[plan.maxRisk] ?? 3) > (levels[allowed ?? 'low'] ?? 0);
  const expired = !!plan.expiresAt && Date.parse(plan.expiresAt) < now;
  const step = nextWriteStep(plan);
  return {
    exceeded,
    expired,
    step,
    requiresStepwise: plan.steps.some((entry) => entry.status === 'pending'),
    executable:
      ['previewed', 'partial'].includes(plan.status) &&
      !!step &&
      !exceeded &&
      !expired,
  };
}

export function managementURL(step: PlanStep): string | undefined {
  if (step.preview?.evidence) return evidenceURL(step.preview.evidence);
  const routes: Record<string, string> = {
    user: '/system/user',
    role: '/system/role',
    menu: '/system/menu',
    dictType: '/system/dict/type',
    dictData: '/system/dict/data',
    config: '/system/config',
    notice: '/system/notice',
    aiProvider: '/system/ai-provider',
    storage: '/system/storage',
    upload: '/system/upload',
    job: '/monitor/job',
    operlog: '/monitor/operlog',
    loginlog: '/monitor/loginlog',
    jobLog: '/monitor/jobLog',
    cache: '/monitor/cache',
    online: '/monitor/online',
  };
  return routes[step.capability.split('.')[1]];
}

/** Only completed plans suggest destinations for steps that actually succeeded. */
export function completedPlanEvidence(plan: Plan): Evidence[] {
  if (plan.status !== 'done') return [];
  return plan.steps
    .filter((step) => step.status === 'applied')
    .flatMap((step) => {
      const route = managementURL(step);
      return route ? [{ route }] : [];
    });
}

export function pageSuggestions(evidence: Evidence[], menus: MenuRoute[]) {
  const pages = flattenPages(normalizeMenus(menus));
  const seen = new Set<string>();
  return evidence.flatMap((entry) => {
    const url = navigationURL(entry, menus);
    if (!url || seen.has(url)) return [];
    seen.add(url);
    const pathname = new URL(url, 'https://intent.invalid').pathname;
    const title =
      pages.find((page) => page.path === pathname)?.meta?.title ?? pathname;
    return [{ url, title }];
  });
}
