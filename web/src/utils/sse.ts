import { t } from '@/i18n';
import { ApiError, auth, Code, type Envelope } from './request';

export interface StreamEvent {
  type: string;
  [key: string]: unknown;
}

/** SSE parser preserves split UTF-8, multiline data and CRLF boundaries. */
export class SSEParser {
  private pending = '';
  constructor(private emit: (event: StreamEvent) => void) {}
  push(chunk: string) {
    this.pending += chunk;
    while (true) {
      const match = /\r?\n\r?\n/.exec(this.pending);
      if (!match) break;
      const frame = this.pending.slice(0, match.index);
      if (frame.length > 8 * 1024 * 1024)
        throw new Error(t('流式事件过大，请缩小查询或计划范围'));
      this.pending = this.pending.slice(match.index + match[0].length);
      const data = frame
        .split(/\r?\n/)
        .filter((line) => line.startsWith('data:'))
        .map((line) => line.slice(5).replace(/^ /, ''))
        .join('\n');
      if (!data || data === '[DONE]') continue;
      const event = JSON.parse(data) as StreamEvent;
      if (typeof event.type === 'string') this.emit(event);
    }
    if (this.pending.length > 8 * 1024 * 1024)
      throw new Error(t('流式事件过大，请缩小查询或计划范围'));
  }
  finish() {
    if (this.pending.trim()) this.push('\n\n');
  }
}

export async function streamChat(
  input: unknown,
  emit: (event: StreamEvent) => void,
  signal: AbortSignal,
): Promise<void> {
  const token = auth.get();
  const response = await fetch(
    `${typeof __API_BASE__ !== 'undefined' ? __API_BASE__ : '/api'}/ai/chat/stream`,
    {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(input),
      signal,
    },
  );
  if (
    !response.ok ||
    !response.headers.get('content-type')?.includes('text/event-stream')
  ) {
    let body: Envelope;
    try {
      body = (await response.json()) as Envelope;
    } catch {
      throw new ApiError(
        response.status || Code.InternalError,
        t('会话服务返回了无法识别的响应（HTTP {value0}）', {
          value0: response.status,
        }),
      );
    }
    if (
      (response.status === 401 || body.code === Code.Unauthorized) &&
      token === auth.get()
    ) {
      auth.clear();
      if (
        typeof location !== 'undefined' &&
        !location.pathname.startsWith('/login')
      )
        location.replace(
          `/login?redirect=${encodeURIComponent(location.pathname + location.search + location.hash)}`,
        );
    }
    throw new ApiError(
      body.code ?? response.status,
      body.msg ?? t('会话请求失败'),
      body.data,
    );
  }
  const reader = response.body?.getReader();
  if (!reader) throw new Error(t('浏览器不支持流式响应'));
  const decoder = new TextDecoder();
  let receivedDone = false,
    completed = false;
  const parser = new SSEParser((event) => {
    if (event.type === 'done') receivedDone = true;
    emit(event);
  });
  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      parser.push(decoder.decode(value, { stream: true }));
    }
    parser.push(decoder.decode());
    parser.finish();
    if (!receivedDone)
      throw new Error(t('流式连接提前结束，可以重新发送消息或检查会话记录'));
    completed = true;
  } finally {
    if (!completed) await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}
