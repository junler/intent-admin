import { Select, Spin } from 'antd';
import { useEffect, useRef, useState } from 'react';
import { api } from '@/utils/request';
import type { Field, Row } from './types';

export function RemoteSelect({
  source,
  value,
  onChange,
}: {
  source: NonNullable<Field['remote']>;
  value?: string | number;
  onChange?: (value?: string | number) => void;
}) {
  const [query, setQuery] = useState('');
  const [options, setOptions] = useState<
    { label: string; value: string | number }[]
  >([]);
  const [selected, setSelected] = useState<{
    label: string;
    value: string | number;
  }>();
  const [loading, setLoading] = useState(false);
  const generation = useRef(0);
  useEffect(() => {
    const current = ++generation.current;
    const timer = setTimeout(async () => {
      setLoading(true);
      try {
        const result = await api.get<Row[] | { rows: Row[] }>(source.url, {
          ...source.params,
          [source.queryKey ?? 'name']: query,
          pageNum: 1,
          pageSize: 20,
        });
        if (generation.current !== current) return;
        setOptions(
          (Array.isArray(result) ? result : result.rows).map((row) => ({
            label: String(row[source.labelKey]),
            value: row[source.valueKey],
          })),
        );
      } catch {
        if (generation.current === current) setOptions([]);
      } finally {
        if (generation.current === current) setLoading(false);
      }
    }, 250);
    return () => {
      clearTimeout(timer);
      generation.current++;
    };
  }, [query, source]);
  useEffect(() => {
    let active = true;
    setSelected(undefined);
    if (value != null && source.resolveUrl) {
      void api
        .get<Row>(
          source.resolveUrl.replace(':id', encodeURIComponent(String(value))),
        )
        .then((row) => {
          if (active)
            setSelected({ value, label: String(row[source.labelKey]) });
        })
        .catch(() => {});
    }
    return () => {
      active = false;
    };
  }, [value, source]);
  const choices =
    selected && !options.some((item) => item.value === selected.value)
      ? [selected, ...options]
      : options;
  return (
    <Select
      allowClear
      showSearch
      filterOption={false}
      value={value}
      onSearch={setQuery}
      onChange={onChange}
      onSelect={(id, option) =>
        setSelected({ value: id, label: String(option.label) })
      }
      options={choices}
      notFoundContent={loading ? <Spin size="small" /> : undefined}
      loading={loading}
    />
  );
}
