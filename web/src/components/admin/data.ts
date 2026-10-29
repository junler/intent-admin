import { t } from '@/i18n';
import { api, type PageData } from '@/utils/request';
import type { Row } from './types';
export async function allRows(base: string, params: Row = {}): Promise<Row[]> {
  const rows: Row[] = [];
  for (let pageNum = 1; pageNum <= 1000; pageNum++) {
    const result = await api.get<PageData<Row> | Row[]>(`${base}/list`, {
      ...params,
      pageNum,
      pageSize: 100,
    });
    if (Array.isArray(result)) return result;
    rows.push(...result.rows);
    if (rows.length >= result.total || result.rows.length === 0) return rows;
  }
  throw new Error(t('查询记录过多，请缩小范围'));
}
