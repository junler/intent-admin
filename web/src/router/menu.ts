// biome-ignore-all lint/suspicious/noControlCharactersInRegex: URL validation deliberately rejects C0 controls and DEL.
/** Routes are issued by getRouters after the backend checks effective permissions. */
export interface MenuRoute {
  path: string;
  component?: string;
  hidden?: boolean;
  external?: boolean;
  query?: string;
  meta?: { title?: string; icon?: string; noCache?: boolean };
  children?: MenuRoute[];
}

const containers = new Set(['Layout', 'ParentView', 'InnerLink']);

function isPage(route: MenuRoute): boolean {
  return Boolean(
    route.external !== true &&
      route.component &&
      !containers.has(route.component),
  );
}

/** External navigation requires an explicit backend flag and a safe absolute web URL. */
export function isSafeExternalUrl(value: string): boolean {
  if (
    !/^https?:\/\//i.test(value) ||
    value.trim() !== value ||
    /[\\\u0000-\u001f\u007f]/.test(value)
  )
    return false;
  try {
    const decoded = decodeURIComponent(value);
    if (/[\u0000-\u001f\u007f]/.test(decoded)) return false;
    const url = new URL(value);
    return (
      (url.protocol === 'http:' || url.protocol === 'https:') &&
      Boolean(url.hostname) &&
      !url.username &&
      !url.password
    );
  } catch {
    return false;
  }
}

function localPath(path: string, parentPath: string): string | undefined {
  if (
    /^[a-z][a-z\d+.-]*:/i.test(path) ||
    path.startsWith('//') ||
    /[\\?#\u0000-\u001f]/.test(path)
  ) {
    return undefined;
  }
  const absolute = path.startsWith('/') ? path : `${parentPath}/${path}`;
  const segments: string[] = [];
  for (const segment of absolute.split('/')) {
    if (!segment || segment === '.') continue;
    if (segment === '..') segments.pop();
    else segments.push(segment);
  }
  return `/${segments.join('/')}`;
}

/** Join relative menu paths once, leaving absolute child paths intact. */
export function normalizeMenus(
  routes: MenuRoute[],
  parentPath = '',
): MenuRoute[] {
  return routes.flatMap((route) => {
    if (route.external === true) {
      if (!isSafeExternalUrl(route.path)) return [];
      return [
        {
          ...route,
          meta: route.meta ? { ...route.meta } : undefined,
          children: undefined,
        },
      ];
    }
    const path = localPath(route.path, parentPath);
    if (!path) return [];
    return [
      {
        ...route,
        path,
        meta: route.meta ? { ...route.meta } : undefined,
        children: route.children
          ? normalizeMenus(route.children, path)
          : undefined,
      },
    ];
  });
}

/** Hidden pages remain registered so an authorized deep link still works. */
export function flattenPages(routes: MenuRoute[]): MenuRoute[] {
  return routes.flatMap((route) => [
    ...(isPage(route) ? [route] : []),
    ...flattenPages(route.children ?? []),
  ]);
}

/** Hiding a directory also hides its descendants from the sidebar. */
export function visibleMenus(routes: MenuRoute[]): MenuRoute[] {
  return routes.flatMap((route) => {
    if (route.hidden) return [];
    if (route.external === true)
      return isSafeExternalUrl(route.path)
        ? [{ ...route, children: undefined }]
        : [];
    if (route.component === 'InnerLink') return [];
    const children = visibleMenus(route.children ?? []);
    if (!isPage(route) && children.length === 0) return [];
    return [{ ...route, children: children.length ? children : undefined }];
  });
}

export function firstPage(routes: MenuRoute[]): string {
  const page = flattenPages(visibleMenus(normalizeMenus(routes)))[0];
  return page?.path ?? '/403';
}

/** Keep the requested deep link without accepting an external login redirect. */
export function safeRedirect(value: string | null, fallback = '/'): string {
  if (
    !value?.startsWith('/') ||
    value.startsWith('//') ||
    /[\\\u0000-\u001f\u007f]/.test(value)
  ) {
    return fallback;
  }
  try {
    const url = new URL(value, 'https://intent.invalid');
    const decodedPath = decodeURIComponent(url.pathname);
    if (
      url.origin !== 'https://intent.invalid' ||
      decodedPath.startsWith('//') ||
      decodedPath.includes('\\') ||
      /^\/login(?:\/|$)/i.test(decodedPath)
    )
      return fallback;
    return `${url.pathname}${url.search}${url.hash}`;
  } catch {
    return fallback;
  }
}
