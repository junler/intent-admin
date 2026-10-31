import { create } from 'zustand';

export interface FormFocus {
  title: string;
  mode: 'create' | 'update' | 'action';
  route?: string;
  path?: string;
  recordId?: string;
  formId?: string;
  field?: string;
}

interface PanelState {
  open: boolean;
  request?: {
    id: number;
    text?: string;
    sessionId?: string;
    draft?: string;
    layer?: number;
    form?: FormFocus;
  };
  show: (text?: string, sessionId?: string) => void;
  compose: (draft: string, form?: FormFocus, layer?: number) => void;
  close: () => void;
  reveal: () => void;
}

let commandId = 0;
export const useAiPanel = create<PanelState>((set) => ({
  open: false,
  show: (text, sessionId) =>
    set({ open: true, request: { id: ++commandId, text, sessionId } }),
  compose: (draft, form, layer) =>
    set({ open: true, request: { id: ++commandId, draft, form, layer } }),
  close: () => set({ open: false }),
  reveal: () => set({ open: true }),
}));
