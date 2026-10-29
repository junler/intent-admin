import { describe, expect, it } from 'vitest';
import { annotateDiffField } from './fieldMetadata';
describe('rollback dictionary annotations', () => {
  it('uses dynamic dictionary identifiers for raw stored status diffs', () => {
    expect(
      annotateDiffField('sys_user', {
        field: 'status',
        before: '1',
        after: '0',
      }),
    ).toMatchObject({
      label: '状态',
      beforeText: 'sys_normal_disable:1',
      afterText: 'sys_normal_disable:0',
    });
    expect(
      annotateDiffField('sys_job', { field: 'status', before: '1', after: '0' })
        .beforeText,
    ).toBe('sys_job_status:1');
  });
  it('preserves explicit metadata supplied by the capability', () => {
    expect(
      annotateDiffField('sys_user', {
        field: 'status',
        label: '自定义状态',
        before: 1,
        after: 0,
        beforeText: '业务标签',
      }),
    ).toMatchObject({ label: '自定义状态', beforeText: '业务标签' });
  });
  it('does not invent semantics for unknown entities or absent values', () => {
    expect(
      annotateDiffField('custom_table', {
        field: 'status',
        before: 1,
        after: 0,
      }).beforeText,
    ).toBeUndefined();
    expect(
      annotateDiffField('sys_user', { field: 'sex', before: null, after: '2' })
        .beforeText,
    ).toBeUndefined();
  });
});
