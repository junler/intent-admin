import { useEffect, useLayoutEffect, useRef } from 'react';
import { usePageActive } from '@/contexts/ActivePageContext';
import { subscribePageRefresh } from '@/utils/pageRefresh';

export function usePageRefresh(refresh: () => unknown | Promise<unknown>) {
  const active = usePageActive();
  const latest = useRef(refresh);
  useLayoutEffect(() => {
    latest.current = refresh;
  }, [refresh]);
  useEffect(() => {
    if (!active) return;
    return subscribePageRefresh(() => latest.current());
  }, [active]);
}
