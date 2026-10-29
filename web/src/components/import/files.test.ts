import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const { request } = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock('@/utils/request', () => ({
  request,
  ApiError: class extends Error {
    constructor(
      readonly code: number,
      message: string,
      readonly detail?: unknown,
    ) {
      super(message);
    }
  },
}));
vi.mock('@/i18n', () => ({ getLocale: () => 'en-US' }));

import { downloadImportFile } from './files';

describe('import workbook downloads', () => {
  const click = vi.fn();
  const remove = vi.fn();
  const create = vi.fn(() => 'blob:import-report');
  const revoke = vi.fn();
  const anchor = { click, remove, href: '', download: '' };

  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal('URL', { createObjectURL: create, revokeObjectURL: revoke });
    vi.stubGlobal('document', {
      createElement: () => anchor,
      body: { append: vi.fn() },
    });
  });
  afterEach(() => vi.unstubAllGlobals());

  it('never saves a failed authorization envelope as an XLSX file', async () => {
    request.mockResolvedValue({
      data: new Blob([JSON.stringify({ code: 403, msg: 'Forbidden' })], {
        type: 'application/json',
      }),
    });
    await expect(
      downloadImportFile('/system/user/import/template', 'template.xlsx'),
    ).rejects.toMatchObject({ code: 403 });
    expect(create).not.toHaveBeenCalled();
    expect(click).not.toHaveBeenCalled();
  });

  it('downloads the owner-scoped workbook and releases its object URL', async () => {
    const workbook = new Blob(['xlsx'], {
      type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    });
    request.mockResolvedValue({ data: workbook });
    await downloadImportFile(
      '/system/user/import/example/errors',
      'errors.xlsx',
    );
    expect(request).toHaveBeenCalledWith({
      url: '/system/user/import/example/errors?locale=en-US',
      method: 'GET',
      responseType: 'blob',
    });
    expect(create).toHaveBeenCalledWith(workbook);
    expect(anchor.download).toBe('errors.xlsx');
    expect(click).toHaveBeenCalledOnce();
    expect(remove).toHaveBeenCalledOnce();
    expect(revoke).toHaveBeenCalledWith('blob:import-report');
  });
});
