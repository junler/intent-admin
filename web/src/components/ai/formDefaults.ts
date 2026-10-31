import type { Field, Row } from '@/components/admin/types';

/** Declared suggestions only; current form values never enter AI metadata. */
export function formDefaults(
  fields: Field[],
  editing = false,
  title?: string,
): Row {
  return Object.fromEntries(
    fields.flatMap((field) => {
      if (
        field.hidden ||
        field.form === false ||
        (editing && field.createOnly) ||
        field.type === 'password' ||
        /password|passwd|salt|token|secret|api.?key|private.?key|credential|_enc|configValue/i.test(
          field.name,
        )
      )
        return [];
      const value =
        field.aiDefaultValue ??
        field.defaultValue ??
        ((field.required || (!editing && field.createRequired)) &&
        field.name !== 'userName' &&
        /^(name|title|.*Name|.*Title)$/.test(field.name)
          ? title
          : undefined);
      return value === undefined ? [] : [[field.name, value]];
    }),
  );
}

/** Defaults fill blanks; values explicitly requested by the user override them. */
export function fillWithDefaults(
  current: Row,
  defaults: Row,
  values: Row,
): Row {
  const patch: Row = {};
  for (const [key, value] of Object.entries(defaults)) {
    if (
      current[key] === undefined ||
      current[key] === null ||
      current[key] === ''
    )
      patch[key] = value;
  }
  return { ...patch, ...values };
}
