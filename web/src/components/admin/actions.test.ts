import { describe, expect, it } from 'vitest';
import {
  cleanForm,
  deletionArgs,
  editFormValues,
  rowsToTree,
  writeFormValues,
} from './actions';
import { validCron } from './cron';

describe('admin action payloads', () => {
  it('preserves numeric resource IDs for batch preview while providing generic IDs', () => {
    expect(deletionArgs('userId', [1, 3])).toEqual({
      ids: [1, 3],
      userIds: [1, 3],
    });
  });
  it('removes form metadata without dropping deliberate false and empty values', () => {
    expect(
      cleanForm({
        status: '0',
        enabled: false,
        secret: '',
        missing: undefined,
        __editing: true,
      }),
    ).toEqual({ status: '0', enabled: false, secret: '' });
  });
  it('builds menu ancestry with deterministic orphan handling', () => {
    const tree = rowsToTree(
      [
        { menuId: 3, parentId: 1 },
        { menuId: 1, parentId: 0 },
        { menuId: 2, parentId: 99 },
      ],
      'menuId',
    );
    expect(tree.map((row) => row.menuId)).toEqual([1, 2]);
    expect(tree[0].children[0].menuId).toBe(3);
  });
});
describe('cron inputs', () => {
  it.each(['0 * * * *', '*/5 9-18 * * MON-FRI', '@every 5m', '@every 1h30m'])(
    'accepts supported schedule %s',
    (value) => expect(validCron(value)).toBe(true),
  );
  it.each([
    '* * * * * *',
    '? * * * *',
    '@every 0s',
    '@every 500ms',
    '0 * * *',
    '',
    '@every',
    '@every 0x5m',
  ])('rejects unsupported schedule %s', (value) =>
    expect(validCron(value)).toBe(false),
  );
});

describe('credential editing', () => {
  const fields = [
    { name: 'apiKey', label: 'Key', type: 'password' as const },
    { name: 'secretKey', label: 'Secret', type: 'password' as const },
  ];
  it('never sends masked credentials back when editing an existing configuration', () => {
    const values = editFormValues(fields, {
      apiKey: 'sk-***last',
      secretKey: '***1234',
      name: 'Existing',
    });
    expect(writeFormValues(fields, values)).toEqual({ name: 'Existing' });
  });
  it('submits an explicitly supplied new credential unchanged', () => {
    expect(
      writeFormValues(fields, {
        name: 'Existing',
        apiKey: 'new-key',
        secretKey: '',
      }),
    ).toEqual({ name: 'Existing', apiKey: 'new-key' });
  });
});
