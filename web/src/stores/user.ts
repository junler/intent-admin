import { create } from 'zustand';
import { useFormDraftStore } from './formDraft';

import type { MenuRoute } from '@/router/menu';
import { normalizeMenus } from '@/router/menu';
import { api, auth } from '@/utils/request';

export interface UserInfo {
  userId: number;
  userName: string;
  nickName: string;
  avatar?: string;
}

export interface Identity {
  user: UserInfo;
  roles: string[];
  permissions: string[];
  aiMaxRisk: string;
}

export interface LoginInput {
  username: string;
  password: string;
  code?: string;
  uuid?: string;
}

interface UserState {
  identity: Identity | null;
  menus: MenuRoute[];
  ready: boolean;
  load: () => Promise<void>;
  signIn: (input: LoginInput) => Promise<void>;
  signOut: () => Promise<void>;
  reset: () => void;
}

let pending: { token: string; promise: Promise<void> } | undefined;

export const useUserStore = create<UserState>((set, get) => ({
  identity: null,
  menus: [],
  ready: false,
  reset: () => {
    useFormDraftStore.getState().clear();
    pending = undefined;
    set({ identity: null, menus: [], ready: false });
  },
  load: () => {
    const token = auth.get();
    if (get().ready) return Promise.resolve();
    if (pending?.token === token) return pending.promise;
    const promise = Promise.all([
      api.get<Identity>('/getInfo'),
      api.get<MenuRoute[]>('/getRouters'),
    ])
      .then(([identity, menus]) => {
        // An earlier account's response must never populate a new session.
        if (token === auth.get()) {
          set({ identity, menus: normalizeMenus(menus), ready: true });
        }
      })
      .finally(() => {
        if (pending?.promise === promise) pending = undefined;
      });
    pending = { token, promise };
    return promise;
  },
  signIn: async (input) => {
    const { token } = await api.post<{ token: string }>('/login', input, {
      ignoreUnauthorized: true,
      dedupe: false,
    });
    get().reset();
    auth.set(token);
    await get().load();
  },
  signOut: async () => {
    await api.post('/logout', undefined, { silent: true });
    auth.clear();
    get().reset();
  },
}));

/** Validate a saved session before leaving the login page. */
export async function resumeSession(): Promise<boolean> {
  if (!auth.get()) return false;
  useUserStore.getState().reset();
  try {
    await useUserStore.getState().load();
    return Boolean(auth.get() && useUserStore.getState().ready);
  } catch {
    useUserStore.getState().reset();
    return false;
  }
}
