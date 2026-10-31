type Refresh = () => unknown | Promise<unknown>;
const handlers = new Set<Refresh>();

/** Pages subscribe only while active, preserving their filters and local state. */
export function subscribePageRefresh(refresh: Refresh) {
  handlers.add(refresh);
  return () => {
    handlers.delete(refresh);
  };
}

/** A failed reload must not prevent the other visible data panels from updating. */
export async function refreshCurrentPage() {
  await Promise.allSettled(
    [...handlers].map((refresh) => Promise.resolve().then(refresh)),
  );
}
