const timestamp =
  /^(\d{4})-(\d{2})-(\d{2})T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.(\d+))?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/;

// The API sends RFC3339 timestamps. Date.parse alone also accepts strings
// without a zone and silently rolls some invalid dates into the next month.
export function parseTokenTimestamp(value: unknown) {
  if (typeof value !== 'string') return;
  const parts = timestamp.exec(value);
  if (!parts || parts[0] !== value) return;
  const [, y, m, d, fraction] = parts;
  const year = Number(y);
  const month = Number(m);
  const day = Number(d);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]) return;
  const ms = Date.parse(value);
  if (!Number.isFinite(ms)) return;
  // Native dates truncate sub-millisecond precision. Compare against the next
  // browser-clock tick so a fractional cutoff isn't treated as expired early.
  const hasSubMillisecondPart = /[1-9]/.test(fraction?.slice(3) ?? '');
  return { date: new Date(ms), cutoffMs: ms + Number(hasSubMillisecondPart) };
}
