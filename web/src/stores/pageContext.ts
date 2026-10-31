import { useEffect } from 'react';
import { create } from 'zustand';
import { usePageActive, usePageLocation } from '@/contexts/ActivePageContext';

/**
 * PageContext is what makes the Omnibox understand "these" and "this".
 *
 * It is a hint, never an authorisation: the backend re-checks permissions per
 * row, because anything the frontend supplies is attacker-controlled.
 */
export interface PageContext {
  route: string;
  /** The page's primary capability, e.g. demo.item.list. */
  capability?: string;
  /** Current editor identity only; form values and credentials are never shared here. */
  form?: { route: string; mode: 'create' | 'update'; recordId?: string };
  /** Current filter form values. */
  filters?: Record<string, unknown>;
  /** Checked row ids, capped to keep the payload bounded. */
  selectedIds?: (string | number)[];
  /** Ids visible on the current page. */
  visibleIds?: (string | number)[];
  /** Readable labels for the selected rows, so the AI can name them back. */
  selectedLabels?: string[];
}

const currentPath = () =>
  typeof location === 'undefined' ? '/' : location.pathname;

const MAX_SELECTED = 500;
const MAX_VISIBLE = 200;

interface Store {
  context: PageContext;
  set: (ctx: PageContext) => void;
  clear: (ownerRoute?: string) => void;
}

export const usePageContextStore = create<Store>((set) => ({
  context: { route: currentPath() },
  set: (ctx) =>
    set({
      context: {
        ...ctx,
        selectedIds: ctx.selectedIds?.slice(0, MAX_SELECTED),
        visibleIds: ctx.visibleIds?.slice(0, MAX_VISIBLE),
        selectedLabels: ctx.selectedLabels?.slice(0, MAX_SELECTED),
      },
    }),
  clear: (ownerRoute) =>
    set((state) =>
      ownerRoute && state.context.route !== ownerRoute
        ? state
        : { context: { route: currentPath() } },
    ),
}));

/**
 * usePageContext reports the current page's context. One call per list page is
 * all that is needed for that page to become AI-addressable.
 */
export function usePageContext(ctx: Omit<PageContext, 'route'>) {
  const active = usePageActive();
  const pageLocation = usePageLocation();
  const route = pageLocation?.pathname ?? currentPath();
  const set = usePageContextStore((s) => s.set);
  const clear = usePageContextStore((s) => s.clear);

  useEffect(() => {
    if (!active) return;
    set({ route, ...ctx });
    return () => clear(route);
    // Serialised comparison keeps this from firing on every render while
    // still reacting to filter and selection changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, route, JSON.stringify(ctx)]);
}
