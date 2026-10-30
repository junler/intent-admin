import { afterEach, describe, expect, it, vi } from 'vitest';
import { getLocale, localizeMessage, translate } from './index';

afterEach(() => vi.unstubAllGlobals());

describe('interface locale', () => {
  it('uses the same persisted locale as Umi and falls back safely', () => {
    vi.stubGlobal('localStorage', { getItem: () => 'en-US' });
    expect(getLocale()).toBe('en-US');
    vi.stubGlobal('localStorage', { getItem: () => 'unexpected' });
    expect(getLocale()).toBe('zh-CN');
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('blocked');
      },
    });
    expect(getLocale()).toBe('zh-CN');
  });

  it('preserves whitespace and interpolates values without interpreting them', () => {
    expect(translate('通知', 'en-US')).toBe('Notifications');
    expect(translate(' 通知 ', 'en-US')).toBe(' Notifications ');
    expect(translate('已选 {value0} 项', 'en-US', { value0: 3 })).toBe(
      '3 selected',
    );
    expect(translate('已选 {value0} 项', 'zh-CN', { value0: 3 })).toBe(
      '已选 3 项',
    );
    expect(translate('自定义业务文案', 'en-US')).toBe('自定义业务文案');
    expect(translate('toString', 'en-US')).toBe('toString');
    expect(translate('__proto__', 'en-US')).toBe('__proto__');
  });

  it('localizes known server templates without altering unrelated content', () => {
    vi.stubGlobal('localStorage', { getItem: () => 'en-US' });
    expect(localizeMessage('已选 7 项')).toBe('7 selected');
    expect(localizeMessage('My custom record')).toBe('My custom record');
  });
});
