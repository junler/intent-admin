import { flattenPages, type MenuRoute } from '@/router/menu';
import type { PageContext } from '@/stores/pageContext';

/** What the Omnibox is currently holding on the user's behalf. */
export interface Selection {
  /** Menu title of the page the rows belong to, when the menu names it. */
  scope?: string;
  count: number;
  /** A single row's readable label, so the bar can name it instead of counting. */
  label?: string;
  /** Labels the assistant can name back, for the chip's tooltip. */
  labels: string[];
}

/** Title of the authorized menu entry that owns a route. */
export function pageTitle(
  menus: MenuRoute[],
  route: string,
): string | undefined {
  return flattenPages(menus).find((page) => page.path === route)?.meta?.title;
}

/**
 * Describe the rows the current page has checked.
 *
 * The result drives the chip inside the bar: it appears only when there is a
 * selection to act on, which is exactly when an intent like "disable these"
 * becomes meaningful. No selection means no chip, not an empty one.
 */
export function selectionOf(
  context: PageContext,
  menus: MenuRoute[],
): Selection | undefined {
  const count = context.selectedIds?.length ?? 0;
  if (!count) return undefined;
  const labels = (context.selectedLabels ?? [])
    .map((label) => label.trim())
    .filter(Boolean);
  return {
    scope: pageTitle(menus, context.route),
    count,
    label: count === 1 ? labels[0] : undefined,
    labels,
  };
}
