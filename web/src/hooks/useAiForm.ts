import { useEffect, useRef } from 'react';
import { create } from 'zustand';
import { usePageActive, usePageLocation } from '@/contexts/ActivePageContext';
import { useFormDraftStore } from '@/stores/formDraft';
import { usePageContextStore } from '@/stores/pageContext';
import { useUserStore } from '@/stores/user';
import { hasPermission } from '@/utils/permission';

interface UiForm {
  name: string;
  summary: string;
  route: string;
  permission: string;
  fields: string[];
}

export const useUiForms = create<{ forms: Record<string, UiForm> }>(() => ({
  forms: {},
}));

/** Register local form controls without exposing field values or submitting them. */
export function useAiForm(options: {
  id: string;
  title: string;
  fields: string[];
  permission?: string;
  enabled?: boolean;
  fill: (values: Record<string, any>) => void | Promise<void>;
}) {
  const pageActive = usePageActive();
  const location = usePageLocation();
  const currentRoute = usePageContextStore((state) => state.context.route);
  const route = location?.pathname ?? currentRoute;
  const active = pageActive && options.enabled !== false;
  const name = `ui.${options.id}`;
  const fill = useRef(options.fill);
  fill.current = options.fill;
  const draft = useFormDraftStore((state) => state.draft);
  const identity = useUserStore((state) => state.identity);
  const fields = JSON.stringify(options.fields);
  const permission = options.permission ?? '';
  useEffect(() => {
    if (
      !active ||
      !identity ||
      !hasPermission(identity.permissions, permission)
    )
      return;
    const descriptor = {
      name,
      summary: options.title,
      route,
      permission,
      fields: JSON.parse(fields),
    };
    useUiForms.setState((state) => ({
      forms: { ...state.forms, [name]: descriptor },
    }));
    return () => {
      useUiForms.setState((state) => {
        const forms = { ...state.forms };
        delete forms[name];
        return { forms };
      });
    };
  }, [active, identity, name, options.title, route, permission, fields]);
  useEffect(() => {
    if (
      !active ||
      !identity ||
      draft?.target.formId !== name ||
      draft.target.route !== route
    )
      return;
    const pending = useFormDraftStore
      .getState()
      .take(identity.user.userId, route);
    if (!pending || !hasPermission(identity.permissions, permission)) return;
    const allowed: string[] = JSON.parse(fields);
    const values = Object.fromEntries(
      Object.entries(pending.values).filter(([key]) => allowed.includes(key)),
    );
    void Promise.resolve(fill.current(values)).catch(() => {});
  }, [active, identity, draft, name, route, permission, fields]);
}
