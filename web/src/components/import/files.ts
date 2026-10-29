import { getLocale } from '@/i18n';
import { ApiError, type Envelope, request } from '@/utils/request';

/** GET downloads must inspect JSON failure envelopes returned as blobs. */
export async function downloadImportFile(url: string, filename: string) {
  const response = await request<{ data: Blob }>({
    url: `${url}${url.includes('?') ? '&' : '?'}locale=${getLocale()}`,
    method: 'GET',
    responseType: 'blob',
  });
  if (response.data.type.includes('json')) {
    const envelope = JSON.parse(await response.data.text()) as Envelope;
    throw new ApiError(envelope.code, envelope.msg, envelope.data);
  }
  const href = URL.createObjectURL(response.data);
  const anchor = document.createElement('a');
  anchor.href = href;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(href);
}
