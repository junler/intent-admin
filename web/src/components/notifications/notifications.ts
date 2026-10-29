export type NoticeID = number | string;
export interface Notice {
  noticeId: NoticeID;
  link?: string;
  readAt?: string;
  noticeTitle: string;
  noticeContent?: string;
  noticeType?: string;
  createBy?: string;
  createTime?: string;
  status?: string;
  uploadId?: string;
  fileName?: string;
  downloadUrl?: string;
}

/** File ownership is checked by /notifications and again by the URL endpoint. */
export function exportUploadId(notice: Notice): string | undefined {
  return notice.uploadId &&
    /^[a-f\d]{8}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{12}$/i.test(
      notice.uploadId,
    )
    ? notice.uploadId
    : undefined;
}

export function safeDownloadURL(
  value: string,
  origin: string,
): string | undefined {
  if (!value || !value.trim()) return undefined;
  try {
    const url = new URL(value, origin);
    return ['http:', 'https:'].includes(url.protocol) ? url.href : undefined;
  } catch {
    return undefined;
  }
}

export function readNoticeIds(value: string | null): NoticeID[] {
  try {
    const parsed: unknown = JSON.parse(value ?? '[]');
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter(
        (item): item is NoticeID =>
          (typeof item === 'number' &&
            Number.isSafeInteger(item) &&
            item > 0) ||
          (typeof item === 'string' &&
            /^[a-f\d]{8}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{4}-[a-f\d]{12}$/i.test(
              item,
            )),
      )
      .slice(0, 200);
  } catch {
    return [];
  }
}
