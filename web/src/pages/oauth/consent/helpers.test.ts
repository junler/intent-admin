import { describe, expect, it } from 'vitest';
import { consentRedirect, consentRequestId, requestedScopes } from './helpers';

describe('OAuth consent navigation', () => {
  it('preserves registered HTTPS and numeric loopback callbacks', () => {
    expect(
      consentRedirect(
        'https://client.example/callback?code=opaque&state=abc',
      )?.searchParams.get('state'),
    ).toBe('abc');
    expect(
      consentRedirect('http://127.0.0.1:32451/callback?code=opaque'),
    ).toBeDefined();
    expect(
      consentRedirect('http://[::1]:32451/callback?error=access_denied'),
    ).toBeDefined();
  });

  it('rejects unsafe or insecure callbacks', () => {
    for (const value of [
      'javascript:alert(1)',
      '//client.example/callback',
      'http://client.example/callback',
      'https://user:password@client.example/',
      'https://client.example/#code=secret',
    ]) {
      expect(consentRedirect(value)).toBeUndefined();
    }
  });

  it('accepts only an opaque request UUID and requested scope subset', () => {
    expect(
      consentRequestId('?request_id=12345678-1234-1234-1234-123456789abc'),
    ).toBe('12345678-1234-1234-1234-123456789abc');
    expect(consentRequestId('?request_id=invalid')).toBeUndefined();
    expect(
      requestedScopes(
        ['mcp:read', 'admin', 'mcp:read'],
        [{ name: 'mcp:read', description: '读取' }],
      ),
    ).toEqual(['mcp:read']);
  });
});
