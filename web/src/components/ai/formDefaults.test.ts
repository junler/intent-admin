import { describe, expect, it } from 'vitest';
import { fillWithDefaults, formDefaults } from './formDefaults';

describe('AI form default drafts', () => {
  it('uses declared defaults and ordinary names without fabricating credentials or identity', () => {
    expect(
      formDefaults(
        [
          { name: 'name', label: '名称', required: true },
          { name: 'maxRisk', label: '风险', defaultValue: 'low' },
          { name: 'password', label: '密码', defaultValue: 'never-send' },
          { name: 'accessToken', label: '令牌', aiDefaultValue: 'never-send' },
          { name: 'configValue', label: '参数', defaultValue: 'never-send' },
          { name: 'userName', label: '账号', required: true },
          { name: 'id', label: 'ID', form: false, defaultValue: 1 },
        ],
        false,
        '个人访问令牌',
      ),
    ).toEqual({ name: '个人访问令牌', maxRisk: 'low' });
  });
  it('fills blanks while preserving entered text, zero, false and selected permissions', () => {
    const current = {
      name: '',
      maxRisk: 'medium',
      count: 0,
      enabled: false,
      scopes: ['own:read'],
    };
    expect(
      fillWithDefaults(
        current,
        {
          name: '个人访问令牌',
          maxRisk: 'low',
          count: 1,
          enabled: true,
          scopes: [],
        },
        {},
      ),
    ).toEqual({ name: '个人访问令牌' });
    expect(
      fillWithDefaults(current, { name: '默认名称' }, { name: '用户指定名称' }),
    ).toEqual({ name: '用户指定名称' });
  });
});
