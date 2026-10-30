import { createContext, type ReactNode, useContext, useMemo } from 'react';
import { UNSAFE_LocationContext } from 'react-router-dom';
export interface PageLocation {
  pathname: string;
  search: string;
  hash: string;
  key: string;
  state: unknown;
}
interface PageActivity {
  active: boolean;
  location?: PageLocation;
}
const ActivePageContext = createContext<PageActivity>({ active: true });
export function usePageActive(): boolean {
  return useContext(ActivePageContext).active;
}
export function usePageLocation(): PageLocation | undefined {
  return useContext(ActivePageContext).location;
}
/** Keep inactive page hooks on their last URL while Umi remains the sole navigator. */
export function FrozenPageBoundary({
  active,
  location,
  children,
}: {
  active: boolean;
  location: PageLocation;
  children: ReactNode;
}) {
  const outer = useContext(UNSAFE_LocationContext);
  const activity = useMemo(() => ({ active, location }), [active, location]);
  const router = useMemo(() => ({ ...outer, location }), [outer, location]);
  return (
    <ActivePageContext.Provider value={activity}>
      <UNSAFE_LocationContext.Provider value={router}>
        <div
          hidden={!active}
          aria-hidden={!active}
          style={{ display: active ? undefined : 'none' }}
        >
          {children}
        </div>
      </UNSAFE_LocationContext.Provider>
    </ActivePageContext.Provider>
  );
}
