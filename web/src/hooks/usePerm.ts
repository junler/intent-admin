import { useUserStore } from '@/stores/user';
import { hasPermission } from '@/utils/permission';

const noPermissions: string[] = [];

export function usePerm(
  required: string | string[],
  mode: 'any' | 'all' = 'any',
): boolean {
  const permissions = useUserStore(
    (s) => s.identity?.permissions ?? noPermissions,
  );
  return hasPermission(permissions, required, mode);
}
