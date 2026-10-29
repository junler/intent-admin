import type { ReactNode } from 'react';

import { usePerm } from '@/hooks/usePerm';

/** UI visibility complements the permission checks in the capability executor. */
export function Auth({
  perm,
  mode = 'any',
  children,
  fallback = null,
}: {
  perm: string | string[];
  mode?: 'any' | 'all';
  children: ReactNode;
  fallback?: ReactNode;
}) {
  return usePerm(perm, mode) ? children : fallback;
}
