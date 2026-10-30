import { useCallback, useEffect, useState } from 'react';
import { t } from '@/i18n';
import { api, auth } from '@/utils/request';
export interface DictItem {
  dictCode: number;
  dictLabel: string;
  dictValue: string;
  listClass?: string;
  cssClass?: string;
}
const cache = new Map<string, DictItem[]>();
const inflight = new Map<string, Promise<DictItem[]>>();
const listeners = new Set<() => void>();
export function invalidateDicts() {
  cache.clear();
  for (const notify of listeners) notify();
}
export function useDict(dictType: string) {
  const [items, setItems] = useState<DictItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [version, setVersion] = useState(0);
  const token = auth.get();
  const reload = useCallback(() => {
    cache.delete(`${token}:${dictType}`);
    setVersion((v) => v + 1);
  }, [dictType, token]);
  useEffect(() => {
    const notify = () => setVersion((v) => v + 1);
    listeners.add(notify);
    return () => {
      listeners.delete(notify);
    };
  }, []);
  useEffect(() => {
    let active = true;
    const key = `${token}:${dictType}`;
    if (cache.has(key)) {
      setItems(cache.get(key)!);
      return;
    }
    setLoading(true);
    let promise = inflight.get(key);
    if (!promise) {
      promise = api
        .get<DictItem[]>(
          `/system/dict/data/type/${encodeURIComponent(dictType)}`,
        )
        .then((data) => {
          cache.set(key, data);
          return data;
        })
        .finally(() => inflight.delete(key));
      inflight.set(key, promise);
    }
    void promise
      .then((data) => {
        if (active) setItems(data);
      })
      .catch(() => {})
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [dictType, token, version]);
  return {
    items: items.map((item) => ({ ...item, dictLabel: t(item.dictLabel) })),
    loading,
    reload,
  };
}
