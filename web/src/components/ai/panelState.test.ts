import { beforeEach, describe, expect, it } from 'vitest';
import { formPrompt } from './formPrompt';
import { useAiPanel } from './panelState';

beforeEach(() => useAiPanel.setState({ open: false, request: undefined }));

describe('AI form entry prompts', () => {
  it('opens the composer with a draft and metadata without requesting an immediate send', () => {
    const form = {
      title: '用户管理',
      mode: 'create' as const,
      route: '/system/user',
      field: 'nickName',
    };
    const prompt = formPrompt(form, '昵称');
    useAiPanel.getState().compose(prompt, form);
    const state = useAiPanel.getState();
    expect(state.open).toBe(true);
    expect(state.request?.draft).toBe(prompt);
    expect(state.request?.text).toBeUndefined();
    expect(state.request?.form).toEqual(form);
    expect(prompt).toContain('用户管理');
    expect(prompt).toContain('昵称');
    expect(state.request?.form).not.toHaveProperty('values');
  });
  it('keeps the edit record identity and supports reopening with a different field', () => {
    const form = {
      title: '角色管理',
      mode: 'update' as const,
      recordId: '42',
      route: '/system/role',
    };
    const first = formPrompt(form, '名称');
    useAiPanel.getState().compose(first, { ...form, field: 'roleName' });
    const id = useAiPanel.getState().request?.id ?? 0;
    useAiPanel.getState().close();
    const second = formPrompt(form, '备注');
    useAiPanel.getState().compose(second, { ...form, field: 'remark' });
    expect(second).toContain('42');
    expect(second).toContain('备注');
    expect(useAiPanel.getState().request?.id).toBeGreaterThan(id);
    expect(useAiPanel.getState().request?.draft).toBe(second);
  });
  it('preserves explicit send and saved-session entry behavior', () => {
    useAiPanel.getState().show('查询用户');
    expect(useAiPanel.getState().request?.text).toBe('查询用户');
    expect(useAiPanel.getState().request?.draft).toBeUndefined();
    useAiPanel.getState().show(undefined, 'saved-session');
    expect(useAiPanel.getState().request?.sessionId).toBe('saved-session');
  });
});
