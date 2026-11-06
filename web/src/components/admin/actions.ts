import dayjs from 'dayjs';
import type { Row } from './types';
export function deletionArgs(idKey: string, ids: unknown[]): Row {
  return {
    ids,
    [`${idKey}s`]: ids,
    ...(ids.length === 1 ? { [idKey]: ids[0] } : {}),
  };
}
export function cleanForm(values: Row): Row {
  return Object.fromEntries(
    Object.entries(values).filter(
      ([key, value]) => !key.startsWith('__') && value !== undefined,
    ),
  );
}
export function rowsToTree(
  rows: Row[],
  idKey: string,
  parentKey = 'parentId',
): Row[] {
  const byId = new Map<string, Row>(
    rows.map((row) => [String(row[idKey]), { ...row, children: [] as Row[] }]),
  );
  const roots: Row[] = [];
  for (const row of byId.values()) {
    const parent = byId.get(String(row[parentKey]));
    if (parent && parent !== row) parent.children.push(row);
    else roots.push(row);
  }
  const prune = (items: Row[], seen = new Set<string>()): Row[] =>
    items
      .filter((row) => !seen.has(String(row[idKey])))
      .map((row) => {
        const next = new Set(seen).add(String(row[idKey]));
        const children = prune(row.children, next);
        return { ...row, children: children.length ? children : undefined };
      });
  return prune(roots);
}

export function menuTreeOptions(rows: Row[]): Row[] {
  return rowsToTree(rows, 'menuId').map(function convert(row): Row {
    return {
      title: row.menuName,
      value: row.menuId,
      children: row.children?.map(convert),
    };
  });
}

/** Credential masks displayed by list APIs never enter write forms. */
export function editFormValues(
  fields: import('./types').Field[],
  record: Row,
): Row {
  const values = { ...record };
  for (const field of fields) {
    if (
      field.type === 'items' &&
      field.fields &&
      Array.isArray(values[field.name])
    )
      values[field.name] = values[field.name].map((row: Row) =>
        editFormValues(field.fields ?? [], row),
      );
    if (field.type === 'password') values[field.name] = '';
    if (field.type === 'date' && values[field.name])
      values[field.name] = dayjs(values[field.name]);
    if (field.type === 'dateRange' && Array.isArray(values[field.name]))
      values[field.name] = values[field.name].map((value: string) =>
        dayjs(value),
      );
    if (field.type === 'json' && typeof values[field.name] !== 'string')
      values[field.name] = values[field.name]
        ? JSON.stringify(values[field.name], null, 2)
        : '';
  }
  return values;
}
export function writeFormValues(
  fields: import('./types').Field[],
  input: Row,
): Row {
  const values = cleanForm(input);
  for (const field of fields) {
    if (
      field.type === 'items' &&
      field.fields &&
      Array.isArray(values[field.name])
    )
      values[field.name] = values[field.name].map((row: Row) =>
        writeFormValues(field.fields ?? [], row),
      );
  }
  for (const field of fields) {
    if (field.type === 'date' && values[field.name]?.toISOString)
      values[field.name] = values[field.name].toISOString();
    if (field.type === 'dateRange') {
      const range = values[field.name]?.map((value: any) =>
        value?.toISOString ? value.toISOString() : value,
      );
      if (field.rangeKeys) {
        delete values[field.name];
        field.rangeKeys.forEach((key, i) => {
          values[key] = range?.[i] ?? '';
        });
      } else if (range) values[field.name] = range;
    }
    if (field.type === 'password' && !values[field.name])
      delete values[field.name];
    if (field.type === 'json' && values[field.name])
      values[field.name] = JSON.parse(values[field.name]);
  }
  return values;
}

/** Only actual editable form fields may be prefilled; hidden IDs and server fields stay protected. */
export function fillableValues(
  fields: import('./types').Field[],
  input: Row,
  editing: boolean,
): Row {
  const values: Row = {};
  for (const field of fields) {
    if (
      field.hidden ||
      field.form === false ||
      (editing && field.createOnly) ||
      !Object.hasOwn(input, field.name)
    )
      continue;
    const value = input[field.name];
    if (field.type === 'json')
      values[field.name] =
        typeof value === 'string' ? value : JSON.stringify(value, null, 2);
    else if (field.type === 'date') {
      if (
        (typeof value === 'string' || typeof value === 'number') &&
        dayjs(value).isValid()
      )
        values[field.name] = dayjs(value);
    } else if (field.type === 'dateRange') {
      if (
        Array.isArray(value) &&
        value.length === 2 &&
        value.every((item) => typeof item === 'string' && dayjs(item).isValid())
      )
        values[field.name] = value.map((item) => dayjs(item));
    } else if (
      field.type === 'items' ||
      field.type === 'file' ||
      field.type === 'image'
    ) {
      // Structured rows and upload IDs remain human-controlled browser values.
      continue;
    } else if (field.type === 'switch') {
      if (typeof value === 'boolean') values[field.name] = value;
    } else if (field.type === 'number') {
      if (typeof value === 'number' && Number.isFinite(value))
        values[field.name] = value;
    } else if (field.type === 'multi' || field.type === 'tree') {
      if (
        Array.isArray(value) &&
        value.every(
          (item) => typeof item === 'string' || typeof item === 'number',
        )
      )
        values[field.name] = value;
    } else if (
      typeof value === 'string' ||
      typeof value === 'number' ||
      value === null
    )
      values[field.name] = value;
  }
  return values;
}

export function matchesFormPath(pattern: string, actual: string): boolean {
  const expected = pattern.split('/');
  const parts = actual.split('/');
  return (
    expected.length === parts.length &&
    expected.every((part, index) =>
      part.startsWith(':') ? !!parts[index] : part === parts[index],
    )
  );
}
