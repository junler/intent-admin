import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('antd', () => ({ message: { error: vi.fn() } }));

import { SSEParser, streamChat } from './sse';

describe('SSE event boundaries', () => {
  it('handles fragmented CRLF, pings and multiline data', () => {
    const events: unknown[] = [];
    const parser = new SSEParser((e) => events.push(e));
    parser.push(': ping\r\n\r\ndata: {"type":"delta",\r');
    parser.push('\ndata: "text":"你好"}\r\n\r');
    parser.push('\ndata: {"type":"done"}\n\n');
    expect(events).toEqual([{ type: 'delta', text: '你好' }, { type: 'done' }]);
  });
  it('does not interpret comments or done sentinels as model text', () => {
    const events: unknown[] = [];
    const parser = new SSEParser((e) => events.push(e));
    parser.push(
      ': ping\n\ndata: [DONE]\n\ndata: {"type":"answer","text":"ok"}',
    );
    parser.finish();
    expect(events).toEqual([{ type: 'answer', text: 'ok' }]);
  });
});

describe('streamed chat transport', () => {
  beforeEach(() => {
    const storage = new Map<string, string>();
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
      removeItem: (key: string) => storage.delete(key),
    });
  });
  it('decodes a multibyte character split across fetch chunks', async () => {
    const raw = new TextEncoder().encode(
      'data: {"type":"delta","text":"你"}\n\ndata: {"type":"done"}\n\n',
    );
    const split = raw.findIndex((value) => value > 127) + 1;
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          new ReadableStream({
            start(controller) {
              controller.enqueue(raw.slice(0, split));
              controller.enqueue(raw.slice(split));
              controller.close();
            },
          }),
          { headers: { 'content-type': 'text/event-stream' } },
        ),
      ),
    );
    const events: unknown[] = [];
    await streamChat(
      {},
      (event) => events.push(event),
      new AbortController().signal,
    );
    expect(events).toEqual([{ type: 'delta', text: '你' }, { type: 'done' }]);
  });
  it('reports a clean but premature end instead of treating partial text as completed', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('data: {"type":"delta","text":"partial"}\n\n', {
          headers: { 'content-type': 'text/event-stream' },
        }),
      ),
    );
    await expect(
      streamChat({}, () => {}, new AbortController().signal),
    ).rejects.toThrow('提前结束');
  });
  it('turns a non-JSON gateway response into a useful connection error', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          new Response('<html>bad gateway</html>', { status: 502 }),
        ),
    );
    await expect(
      streamChat({}, () => {}, new AbortController().signal),
    ).rejects.toThrow('HTTP 502');
  });
});
