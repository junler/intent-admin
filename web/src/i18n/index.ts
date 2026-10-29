import messages from '@/locales/en-US';

export type Locale = 'zh-CN' | 'en-US';
export type MessageValues = Record<string, string | number>;

function lookup(source: string): string | undefined {
  return Object.hasOwn(messages, source) ? messages[source] : undefined;
}

// Umi's locale switcher reloads the document after saving this key. Reading
// the same key also localizes module-level table definitions consistently.
export function getLocale(): Locale {
  try {
    return globalThis.localStorage?.getItem('umi_locale') === 'en-US'
      ? 'en-US'
      : 'zh-CN';
  } catch {
    return 'zh-CN';
  }
}

export function translate(
  source: string,
  locale: Locale,
  values?: MessageValues,
): string {
  const trimmed = source.trim();
  const translation = lookup(source) || lookup(trimmed);
  const translated =
    locale === 'en-US' && translation
      ? `${source.match(/^\s*/)?.[0] ?? ''}${translation}${source.match(/\s*$/)?.[0] ?? ''}`
      : source;
  return translated.replace(/\{([\w]+)\}/g, (placeholder, name: string) =>
    values && Object.hasOwn(values, name) ? String(values[name]) : placeholder,
  );
}

export function t(source: string, values?: MessageValues): string {
  return translate(source, getLocale(), values);
}

// Only trusted UI descriptors and API error text pass through this helper;
// user-entered content and returned business data are left unchanged.
export function localizeMessage(source: string): string {
  if (getLocale() !== 'en-US') return source;
  const exact = lookup(source);
  if (exact) return exact;
  for (const [template, translation] of Object.entries(messages)) {
    if (!template.includes('{') || !translation) continue;
    const names: string[] = [];
    const pattern = template
      .split(/(\{\w+\})/)
      .map((part) => {
        const name = /^\{(\w+)\}$/.exec(part);
        if (name) {
          names.push(name[1]);
          return '(.+?)';
        }
        return part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      })
      .join('');
    const match = new RegExp(`^${pattern}$`, 'u').exec(source);
    if (match)
      return translate(
        template,
        'en-US',
        Object.fromEntries(names.map((name, i) => [name, match[i + 1]])),
      );
  }
  return source;
}

// Translate descriptions supplied by trusted metadata endpoints. Identifiers,
// example arguments and returned business records retain their original values.
export function localizeDescriptor<T>(value: T): T {
  const copyFields = new Set([
    'summary',
    'description',
    'label',
    'rule',
    'hint',
    'utterance',
    'title',
  ]);
  const arrays = new Set(['notes', 'rules', 'warnings']);
  function visit(input: unknown, key = '', dictionary = false): unknown {
    if (typeof input === 'string')
      return dictionary || copyFields.has(key) || arrays.has(key)
        ? t(input)
        : input;
    if (Array.isArray(input))
      return input.map((item) => visit(item, key, dictionary));
    if (input && typeof input === 'object')
      return Object.fromEntries(
        Object.entries(input).map(([name, child]) => [
          name,
          visit(child, name, dictionary || name === 'values'),
        ]),
      );
    return input;
  }
  return visit(value) as T;
}
