/** Match explicit permission strings; only the superadmin wildcard is special. */
export function hasPermission(
  permissions: string[],
  required: string | string[],
  mode: 'any' | 'all' = 'any',
): boolean {
  const requested = (Array.isArray(required) ? required : [required]).filter(
    Boolean,
  );
  if (!requested.length || permissions.includes('*:*:*')) return true;
  const granted = new Set(permissions);
  return mode === 'all'
    ? requested.every((permission) => granted.has(permission))
    : requested.some((permission) => granted.has(permission));
}
