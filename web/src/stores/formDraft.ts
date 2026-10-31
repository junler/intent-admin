import { create } from 'zustand';

export interface DraftTarget {
  route: string;
  formId?: string;
  mode: 'create' | 'update' | 'action';
  path?: string;
  recordId?: string;
  permission?: string;
}

let expiryTimer: ReturnType<typeof setTimeout> | undefined;

interface FormDraft {
  owner: number;
  target: DraftTarget;
  values: Record<string, unknown>;
  expiresAt: number;
}

/** One-use browser memory only. Draft values never enter URLs or browser storage. */
export const useFormDraftStore = create<{
  draft?: FormDraft;
  stage: (owner: number, values: unknown, target?: DraftTarget) => void;
  take: (owner: number, route?: string) => FormDraft | undefined;
  clear: () => void;
}>((set, get) => ({
  stage: (
    owner,
    values,
    target = {
      route: '/system/user',
      mode: 'create',
      permission: 'system:user:add',
    },
  ) => {
    if (
      !owner ||
      !values ||
      typeof values !== 'object' ||
      Array.isArray(values)
    )
      return;
    if (JSON.stringify(values).length > 16_384) return;
    const safe = Object.fromEntries(
      Object.entries(values).filter(([key]) =>
        /^[A-Za-z][A-Za-z0-9_]*$/.test(key),
      ),
    );
    clearTimeout(expiryTimer);
    const draft: FormDraft = {
      owner,
      target,
      values: safe,
      expiresAt: Date.now() + 120_000,
    };
    set({ draft });
    expiryTimer = setTimeout(() => {
      if (get().draft === draft) set({ draft: undefined });
    }, 120_000);
  },
  take: (owner, route) => {
    const draft = get().draft;
    if (draft && route && draft.target.route !== route) return undefined;
    clearTimeout(expiryTimer);
    set({ draft: undefined });
    return draft && draft.owner === owner && draft.expiresAt > Date.now()
      ? draft
      : undefined;
  },
  clear: () => {
    clearTimeout(expiryTimer);
    set({ draft: undefined });
  },
}));
