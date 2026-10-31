import { describe, expect, it } from 'vitest';
import { chooseAssistantContext } from './floatingContext';

describe('global AI entry context', () => {
  it('recognizes a create dialog and opens above its mask', () => {
    const result = chooseAssistantContext('/system/user', '用户管理', [
      {
        title: '新增用户',
        layer: 1100,
        hasInputs: true,
        forms: [{ title: '用户管理', mode: 'create' }],
      },
    ]);
    expect(result.form?.mode).toBe('create');
    expect(result.form?.route).toBe('/system/user');
    expect(result.prompt).toContain('用户管理');
    expect(result.layer).toBeGreaterThan(1100);
  });
  it('takes the topmost editor identity instead of an older dialog or inline form', () => {
    const result = chooseAssistantContext(
      '/system/role',
      '角色管理',
      [
        {
          title: '新增角色',
          layer: 1000,
          hasInputs: true,
          forms: [{ title: '角色管理', mode: 'create' }],
        },
        {
          title: '编辑角色',
          layer: 2200,
          hasInputs: true,
          forms: [{ title: '角色管理', mode: 'update', recordId: '42' }],
        },
      ],
      [{ title: '旧表单', mode: 'update', recordId: '9' }],
    );
    expect(result.form?.recordId).toBe('42');
    expect(result.prompt).toContain('42');
    expect(result.layer).toBe(2300);
    expect(result.form).not.toHaveProperty('values');
  });
  it('uses the last opened dialog at equal layers and does not fill an obscured editor', () => {
    const result = chooseAssistantContext('/system/user', '用户管理', [
      {
        title: '新增用户',
        layer: 1000,
        hasInputs: true,
        forms: [{ title: '用户管理', mode: 'create' }],
      },
      { title: '保存确认', layer: 1000, hasInputs: false, forms: [] },
    ]);
    expect(result.form).toBeUndefined();
    expect(result.prompt).toContain('保存确认');
  });
  it('recognizes an inline search form and prepares a query instruction', () => {
    const result = chooseAssistantContext(
      '/system/user',
      '用户管理',
      [],
      [{ title: '用户管理', mode: 'query', formId: 'ui.search.user' }],
    );
    expect(result.form?.mode).toBe('query');
    expect(result.form?.formId).toBe('ui.search.user');
    expect(result.prompt).toBe('请帮我查询“用户管理”表单');
  });
  it('supports visible inline editors and custom dialogs without registered metadata', () => {
    const inline = chooseAssistantContext(
      '/system/profile',
      '个人中心',
      [],
      [{ title: '基本资料', mode: 'update', path: '/system/user/profile' }],
    );
    expect(inline.form?.path).toBe('/system/user/profile');
    const dialog = chooseAssistantContext('/system/config', '参数', [
      { title: '修改参数', layer: 1300, hasInputs: true, forms: [] },
    ]);
    expect(dialog.form?.mode).toBe('update');
  });
});
