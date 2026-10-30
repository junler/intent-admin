import { Tag } from 'antd';
import { useDict } from '@/hooks/useDict';
export function DictTag({ type, value }: { type: string; value: unknown }) {
  const { items } = useDict(type);
  const item = items.find((entry) => entry.dictValue === String(value));
  return (
    <Tag color={item?.listClass || 'default'}>
      {item?.dictLabel ?? String(value ?? '—')}
    </Tag>
  );
}
