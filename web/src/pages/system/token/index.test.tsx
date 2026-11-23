import { App } from 'antd';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Field } from '@/components/admin/types';
import Tokens from './index';

const { captured } = vi.hoisted(() => ({
  captured: { fields: [] as Field[] },
}));

vi.mock('@/components/admin/CrudPage', () => ({
  CrudPage: ({ fields }: { fields: Field[] }) => {
    captured.fields = fields;
    return null;
  },
}));

function loadCells(locale: string) {
  vi.stubGlobal('localStorage', { getItem: () => locale });
  renderToStaticMarkup(
    <App>
      <Tokens />
    </App>,
  );
  const render = (field: string, value: unknown) => {
    const output = captured.fields.find((item) => item.name === field);
    if (!output?.render) throw new Error(`Missing renderer for ${field}`);
    const node = output.render(value, { [field]: value });
    return renderToStaticMarkup(node);
  };
  return {
    expiry: (value: unknown) => render('expiresAt', value),
    lastUsed: (value: unknown) => render('lastUsedAt', value),
  };
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('personal access token timestamps', () => {
  it.each([
    ['zh-CN', '不过期', '未记录', '时间不可用', '已过期'],
    ['en-US', 'No expiration', 'Not recorded', 'Time unavailable', 'Expired'],
  ])(
    'renders precise list states in %s',
    (locale, never, missing, unknown, expired) => {
      vi.useFakeTimers();
      vi.setSystemTime(new Date('2026-11-23T00:00:00Z'));
      const { expiry, lastUsed } = loadCells(locale);
      expect(expiry(null)).toBe(never);
      expect(expiry(undefined)).toBe(unknown);
      expect(lastUsed(null)).toBe(missing);
      expect(lastUsed(undefined)).toBe(missing);

      for (const malformed of [
        '',
        0,
        '2026-11-23',
        '2026-11-23T00:00:00',
        '2026-02-29T00:00:00Z',
        '2026-11-23T24:00:00Z',
      ]) {
        expect(expiry(malformed)).toBe(unknown);
        expect(lastUsed(malformed)).toBe(unknown);
      }

      const before = '2026-11-22T23:59:59.999Z';
      const equal = '2026-11-23T08:00:00+08:00';
      const after = '2026-11-23T00:00:00.001Z';
      const fractionalAfter = '2026-11-23T00:00:00.000001Z';
      for (const timestamp of [before, equal]) {
        const cell = expiry(timestamp);
        expect(cell).toContain(expired);
        expect(cell).toContain(new Date(timestamp).toLocaleString(locale));
      }
      expect(expiry(after)).toBe(new Date(after).toLocaleString(locale));
      expect(expiry(fractionalAfter)).toBe(
        new Date(fractionalAfter).toLocaleString(locale),
      );
      expect(lastUsed(equal)).toBe(new Date(equal).toLocaleString(locale));
    },
  );
});
