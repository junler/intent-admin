import { describe, expect, it } from 'vitest';
import { readCrudQuery, writeCrudQuery } from './query';
import type { Field } from './types';

const fields: Field[] = [
  { name: 'status', label: 'Status', search: true },
  { name: 'jobId', label: 'Job', type: 'number', search: true },
  { name: 'password', label: 'Password', type: 'password' },
];
describe('active CRUD URL filters', () => {
  it('initializes only declared search controls and validates pagination before the first request', () => {
    expect(
      readCrudQuery(
        fields,
        '?status=0&jobId=42&pageNum=3&pageSize=999&password=ignored',
      ),
    ).toEqual({
      filters: { status: '0', jobId: 42 },
      page: { pageNum: 3, pageSize: 100 },
    });
  });
  it('retains explicit blank filters so menu defaults do not reapply after reset', () => {
    expect(
      writeCrudQuery(
        fields,
        '?unrelated=keep',
        {},
        { pageNum: 1, pageSize: 10 },
      ),
    ).toBe('?unrelated=keep&status=&jobId=&pageNum=1&pageSize=10');
  });
  it('ignores invalid numeric controls and pagination', () => {
    expect(
      readCrudQuery(fields, '?jobId=oops&pageNum=-2&pageSize=nope'),
    ).toEqual({ filters: {}, page: { pageNum: 1, pageSize: 10 } });
  });
});
