import type { Evidence } from '@/api/types';
import type { MenuRoute } from '@/router/menu';
import { navigationURL, pageSuggestions } from './state';

/** Only explicit navigation events enter this flow. Answer evidence remains optional. */
export function createChatNavigation(actions: {
  getMenus: () => MenuRoute[];
  navigate: (url: string) => void;
  close: () => void;
  notify: (title: string) => void;
}) {
  let pending: string | undefined;
  return {
    request(evidence: Evidence) {
      pending = navigationURL(evidence, actions.getMenus());
      return pending;
    },
    cancel() {
      pending = undefined;
    },
    complete() {
      const destination = pending
        ? pageSuggestions([{ route: pending }], actions.getMenus())[0]
        : undefined;
      pending = undefined;
      if (!destination) return;
      actions.navigate(destination.url);
      actions.close();
      actions.notify(destination.title);
    },
  };
}
