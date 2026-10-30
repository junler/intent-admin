export interface ConsentScope {
  name: string;
  description: string;
}

export function consentRequestId(search: string): string | undefined {
  const value = new URLSearchParams(search).get('request_id');
  return value &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
      value,
    )
    ? value
    : undefined;
}

// The server validates the registered redirect; this guard also rejects unsafe
// navigation if an unexpected response reaches the consent page.
export function consentRedirect(value: string): URL | undefined {
  try {
    const url = new URL(value);
    if (url.username || url.password || url.hash) return undefined;
    const loopback =
      /^127\.(?:\d{1,3}\.){2}\d{1,3}$/.test(url.hostname) ||
      url.hostname === '[::1]';
    return url.protocol === 'https:' || (url.protocol === 'http:' && loopback)
      ? url
      : undefined;
  } catch {
    return undefined;
  }
}

export function requestedScopes(
  selected: string[],
  scopes: ConsentScope[],
): string[] {
  const allowed = new Set(scopes.map((scope) => scope.name));
  return [...new Set(selected)].filter((name) => allowed.has(name));
}
