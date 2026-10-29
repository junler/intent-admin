import { hasPermission } from '@/utils/permission';
import type { Identity } from '@/stores/user';
export default function access(initialState: { identity?: Identity | null } | undefined) {
  const permissions = initialState?.identity?.permissions ?? [];
  return { canAdmin: permissions.includes('*:*:*'), canUseAI: hasPermission(permissions, 'ai:agent:use'), can: (perm: string) => hasPermission(permissions, perm) };
}
