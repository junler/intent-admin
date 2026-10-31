import { DeleteOutlined, MoreOutlined } from '@ant-design/icons';
import { history } from '@umijs/max';
import {
  Alert,
  App,
  Button,
  Drawer,
  Dropdown,
  Empty,
  Input,
  Select,
  Space,
  Spin,
  Typography,
} from 'antd';
import { useCallback, useEffect, useId, useRef, useState } from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { Evidence } from '@/api/types';
import { Loading } from '@/components/common/Loading';
import { usePerm } from '@/hooks/usePerm';
import { useUiForms } from '@/hooks/useAiForm';
import { getLocale, t } from '@/i18n';
import { usePageContextStore } from '@/stores/pageContext';
import { useUserStore } from '@/stores/user';
import { useFormDraftStore, type DraftTarget } from '@/stores/formDraft';
import { hasPermission } from '@/utils/permission';
import { api } from '@/utils/request';
import { type StreamEvent, streamChat } from '@/utils/sse';
import { type Plan, PlanCard } from './PlanCard';
import { PageSuggestions } from './PageSuggestions';
import { createChatNavigation } from './navigation';
import { useAiPanel, type FormFocus } from './panelState';
export { useAiPanel } from './panelState';

const markdownComponents: Components = {
  table: ({ children }) => (
    <div className="intent-markdown-table">
      <table>{children}</table>
    </div>
  ),
};

interface ChatMessage {
  id: string;
  role: string;
  content: string;
  planId?: string;
  plan?: Plan;
  planUnavailable?: boolean;
  evidence?: Evidence[];
  navigated?: boolean;
}
interface Session {
  id: string;
  title: string;
  model: string;
  messages: ChatMessage[];
}
interface Models {
  providerId: number;
  name: string;
  defaultModel: string;
  models: string[];
  isDefault: boolean;
}
interface ActiveRun {
  id: number;
  controller: AbortController;
  sessionId?: string;
}
// The data-sharing notice is a disclosure, not a warning: it earns the space
// once per browser, then collapses out of the way.
const NOTICE_KEY = 'intent-admin:ai-notice-read';
function noticeRead(): boolean {
  try {
    return globalThis.localStorage?.getItem(NOTICE_KEY) === '1';
  } catch {
    return false;
  }
}
function markNoticeRead() {
  try {
    globalThis.localStorage?.setItem(NOTICE_KEY, '1');
  } catch {
    /* Private browsing just shows the notice again next time. */
  }
}

function evidence(value: unknown): Evidence[] {
  return Array.isArray(value)
    ? value.filter(
        (entry): entry is Evidence =>
          !!entry &&
          typeof entry === 'object' &&
          typeof entry.route === 'string',
      )
    : [];
}

export function AiPanel() {
  const { open, request: incoming, close } = useAiPanel();
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionId, setSessionId] = useState<string>();
  const [models, setModels] = useState<Models[]>([]);
  const [model, setModel] = useState<string>();
  const [text, setText] = useState('');
  const [formFocus, setFormFocus] = useState<FormFocus>();
  const [busy, setBusy] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [tool, setTool] = useState('');
  const [error, setError] = useState('');
  const active = useRef<ActiveRun | undefined>(undefined);
  const sequence = useRef(0);
  const loadSequence = useRef(0);
  const handled = useRef(0);
  const messageViewport = useRef<HTMLDivElement>(null);
  const followOutput = useRef(true);
  const composer = useRef<React.ComponentRef<typeof Input.TextArea>>(null);
  const allowed = usePerm('ai:agent:use');
  const [notice, setNotice] = useState(() => !noticeRead());
  const [composerFocus, setComposerFocus] = useState(false);
  const sessionFieldId = useId();
  const modelFieldId = useId();
  const { message, modal, notification } = App.useApp();
  const refreshSessions = useCallback(
    () =>
      api
        .get<Session[]>('/ai/sessions', undefined, {
          silent: true,
          dedupe: false,
        })
        .then(setSessions)
        .catch(() => {}),
    [],
  );

  const stopActive = useCallback((reset = true) => {
    const run = active.current;
    active.current = undefined;
    if (run) {
      run.controller.abort();
      if (run.sessionId)
        void api
          .post(
            '/ai/stop',
            { sessionId: run.sessionId },
            { silent: true, dedupe: false },
          )
          .catch(() => {});
    }
    if (reset) {
      setBusy(false);
      setTool('');
    }
  }, []);

  useEffect(() => {
    if (!open || !allowed) return;
    let mounted = true;
    void refreshSessions();
    void api
      .get<Models[]>('/ai/models', undefined, { silent: true, dedupe: false })
      .then((items) => {
        if (!mounted) return;
        setModels(items);
        const provider = items.find((item) => item.isDefault) ?? items[0];
        if (provider)
          setModel(
            (old) => old ?? `${provider.providerId}:${provider.defaultModel}`,
          );
      })
      .catch((reason: unknown) => {
        if (mounted)
          setError(
            reason instanceof Error ? reason.message : t('模型列表加载失败'),
          );
      });
    return () => {
      mounted = false;
    };
  }, [open, allowed, refreshSessions]);
  const lastMessage = messages.at(-1);
  const scrollAnchor = `${lastMessage?.id}:${lastMessage?.content}:${lastMessage?.plan?.status}:${tool}`;
  const scrollToLatest = useCallback(() => {
    const viewport = messageViewport.current;
    if (followOutput.current && viewport) {
      // Stream updates must not leave a smooth animation fighting manual scrolling.
      viewport.scrollTop = viewport.scrollHeight;
    }
  }, []);
  useEffect(() => {
    if (open && !restoring) scrollToLatest();
  }, [open, restoring, scrollAnchor, scrollToLatest]);
  useEffect(() => {
    // Hiding the drawer leaves its stream running; revoked access still stops it.
    if (!allowed) stopActive();
  }, [allowed, stopActive]);
  useEffect(
    () => () => {
      loadSequence.current++;
      stopActive(false);
    },
    [stopActive],
  );

  const newConversation = useCallback(() => {
    stopActive();
    followOutput.current = true;
    loadSequence.current++;
    setRestoring(false);
    setSessionId(undefined);
    setMessages([]);
    setError('');
  }, [stopActive]);
  const loadSession = useCallback(
    async (id: string) => {
      stopActive();
      followOutput.current = true;
      setFormFocus(undefined);
      const load = ++loadSequence.current;
      setRestoring(true);
      setError('');
      try {
        const session = await api.get<Session>(
          `/ai/sessions/${encodeURIComponent(id)}`,
          undefined,
          { dedupe: false, silent: true },
        );
        const ids = [
          ...new Set(
            (session.messages ?? [])
              .map((item) => item.planId)
              .filter((value): value is string => !!value),
          ),
        ];
        const plans = new Map<string, Plan>();
        await Promise.all(
          ids.map(async (planId) => {
            try {
              plans.set(
                planId,
                await api.get<Plan>(
                  `/ai/plans/${encodeURIComponent(planId)}`,
                  undefined,
                  { silent: true, dedupe: false },
                ),
              );
            } catch {
              /* The message remains visible even if its plan is unavailable. */
            }
          }),
        );
        if (load !== loadSequence.current) return;
        setSessionId(session.id);
        setMessages(
          (session.messages ?? []).map((item) => ({
            ...item,
            plan: item.planId ? plans.get(item.planId) : undefined,
            planUnavailable: !!item.planId && !plans.has(item.planId),
          })),
        );
        setModel(session.model || undefined);
      } catch (reason) {
        if (load === loadSequence.current)
          setError(
            reason instanceof Error ? reason.message : t('会话加载失败'),
          );
      } finally {
        if (load === loadSequence.current) setRestoring(false);
      }
    },
    [stopActive],
  );

  const send = useCallback(
    async (input: string) => {
      if (!input.trim() || !allowed) return;
      if (restoring) {
        setText(input);
        message.info(t('正在恢复会话，完成后可以发送此消息'));
        return;
      }
      if (active.current) {
        message.info(t('当前会话正在生成，可先停止再发送新消息'));
        return;
      }
      const assistantId = crypto.randomUUID();
      const run: ActiveRun = {
        id: ++sequence.current,
        controller: new AbortController(),
        sessionId,
      };
      active.current = run;
      followOutput.current = true;
      const focusedForm = formFocus;
      setFormFocus(undefined);
      setText('');
      setError('');
      setBusy(true);
      setMessages((old) => [
        ...old,
        { id: crypto.randomUUID(), role: 'user', content: input },
        { id: assistantId, role: 'assistant', content: '' },
      ]);
      const current = () => active.current?.id === run.id;
      const update = (fn: (item: ChatMessage) => ChatMessage) => {
        if (current())
          setMessages((old) =>
            old.map((item) => (item.id === assistantId ? fn(item) : item)),
          );
      };
      const navigation = createChatNavigation({
        getMenus: () => useUserStore.getState().menus,
        navigate: (url) => history.push(url),
        close,
        notify: (title) =>
          notification.success({
            title: t('已打开{page}', { page: t(title) }),
            placement: 'bottomRight',
            duration: 3,
            role: 'status',
            className: 'intent-ai-navigation-toast',
          }),
      });
      let pendingForm:
        | {
            owner: number;
            values: unknown;
            defaults: unknown;
            target: DraftTarget;
          }
        | undefined;
      function onEvent(event: StreamEvent) {
        if (!current()) return;
        if (event.type === 'start') {
          run.sessionId = String(event.sessionId);
          setSessionId(run.sessionId);
        } else if (event.type === 'delta')
          update((item) => ({
            ...item,
            content: item.content + String(event.text ?? ''),
          }));
        else if (event.type === 'answer')
          update((item) => ({
            ...item,
            content: String(event.text ?? item.content),
            evidence: evidence(event.evidence),
          }));
        else if (event.type === 'tool_call')
          setTool(String(event.summary ?? event.name ?? t('正在查询')));
        else if (event.type === 'tool_result') setTool('');
        else if (event.type === 'plan')
          update((item) => ({
            ...item,
            planId: String(event.id),
            plan: event as unknown as Plan,
          }));
        else if (event.type === 'form_fill') {
          const identity = useUserStore.getState().identity;
          const route = String(event.route ?? '');
          const permission =
            typeof event.permission === 'string'
              ? event.permission
              : 'system:user:add';
          const mode =
            event.mode === 'update' ||
            event.mode === 'action' ||
            event.mode === 'query'
              ? event.mode
              : 'create';
          if (!identity || !hasPermission(identity.permissions, permission)) {
            setError(t('表单不存在或无权填写'));
            return;
          }
          const formId =
            typeof event.formId === 'string' ? event.formId : undefined;
          if (formId) {
            const currentForm = useUiForms.getState().forms[formId];
            if (!currentForm || currentForm.route !== route) {
              setError(t('表单未打开或当前账号无权填写'));
              return;
            }
          } else if (!navigation.request({ route })) {
            setError(t('页面不存在或无权访问'));
            return;
          }
          pendingForm = {
            owner: identity.user.userId,
            values: event.values,
            defaults: event.defaults,
            target: {
              route,
              mode,
              formId:
                typeof event.formId === 'string' ? event.formId : undefined,
              path: String(event.path ?? route),
              recordId: String(event.recordId ?? ''),
              permission,
            },
          };
          update((item) => ({
            ...item,
            navigated: true,
            content: t('已填充表单，请检查后确认提交；尚未保存。'),
          }));
        } else if (event.type === 'navigate') {
          const url = navigation.request({
            route: String(event.route ?? ''),
            filters:
              event.filters && typeof event.filters === 'object'
                ? (event.filters as Record<string, unknown>)
                : undefined,
          });
          if (url) {
            update((item) => ({
              ...item,
              content: String(event.note ?? t('已打开筛选后的页面')),
              evidence: [{ route: url }],
              navigated: true,
            }));
          } else {
            setError(t('页面不存在或无权访问'));
          }
        } else if (event.type === 'error') {
          navigation.cancel();
          pendingForm = undefined;
          setError(String(event.message ?? t('生成失败')));
        } else if (event.type === 'done') {
          void refreshSessions();
          if (pendingForm) {
            const identity = useUserStore.getState().identity;
            if (
              identity?.user.userId !== pendingForm.owner ||
              !hasPermission(
                identity.permissions,
                pendingForm.target.permission ?? '',
              )
            ) {
              pendingForm = undefined;
              navigation.cancel();
              return;
            }
            useFormDraftStore
              .getState()
              .stage(
                pendingForm.owner,
                pendingForm.values,
                pendingForm.target,
                pendingForm.defaults,
              );
            pendingForm = undefined;
          }
          navigation.complete();
        }
      }
      try {
        await streamChat(
          {
            sessionId: run.sessionId,
            model,
            message: input,
            pageContext: {
              ...usePageContextStore.getState().context,
              focusedForm,
              forms: Object.values(useUiForms.getState().forms).slice(0, 40),
            },
            locale: getLocale(),
          },
          onEvent,
          run.controller.signal,
        );
      } catch (reason) {
        if (current() && !run.controller.signal.aborted)
          setError(reason instanceof Error ? reason.message : t('连接失败'));
      } finally {
        if (current()) {
          active.current = undefined;
          setBusy(false);
          setTool('');
        }
      }
    },
    [
      allowed,
      restoring,
      message,
      notification,
      sessionId,
      model,
      refreshSessions,
      close,
      formFocus,
    ],
  );

  useEffect(() => {
    if (!incoming || incoming.id === handled.current || !allowed) return;
    handled.current = incoming.id;
    if (incoming.draft !== undefined) {
      setText(incoming.draft);
      setFormFocus(incoming.form);
      composer.current?.focus({ preventScroll: true });
    } else if (incoming.sessionId) void loadSession(incoming.sessionId);
    else if (incoming.text) void send(incoming.text);
    else newConversation();
  }, [incoming, allowed, loadSession, send, newConversation]);

  return (
    <Drawer
      className="intent-ai-drawer"
      zIndex={incoming?.layer ?? 1800}
      placement="right"
      size="min(640px, 100vw)"
      mask={{ closable: true }}
      open={open}
      destroyOnHidden={false}
      aria-label={t('AI 会话')}
      afterOpenChange={(visible) => {
        if (visible && allowed) {
          composer.current?.focus({ preventScroll: true });
          scrollToLatest();
        }
      }}
      onClose={close}
      title={
        <div className="intent-ai-drawer-title">
          {allowed && (
            <div className="intent-ai-controls">
              <div className="intent-ai-field">
                <label
                  className="intent-ai-field-label"
                  htmlFor={sessionFieldId}
                >
                  {t('历史会话')}
                </label>
                <Select
                  id={sessionFieldId}
                  placeholder={t('新的会话')}
                  value={sessionId}
                  popupMatchSelectWidth={false}
                  allowClear
                  options={sessions.map((session) => ({
                    value: session.id,
                    label: session.title,
                  }))}
                  onChange={(id) =>
                    id ? void loadSession(id) : newConversation()
                  }
                />
              </div>
              <div className="intent-ai-field">
                <label className="intent-ai-field-label" htmlFor={modelFieldId}>
                  {t('模型')}
                </label>
                <Select
                  id={modelFieldId}
                  placeholder={t('选择模型')}
                  value={model}
                  showSearch
                  optionFilterProp="label"
                  // Provider/model pairs run long; let the menu outgrow the box
                  // instead of truncating every name.
                  popupMatchSelectWidth={false}
                  options={models.flatMap((provider) =>
                    [
                      ...new Set(
                        provider.models?.length
                          ? provider.models
                          : [provider.defaultModel],
                      ),
                    ].map((name) => ({
                      value: `${provider.providerId}:${name}`,
                      label: `${provider.name} / ${name}`,
                    })),
                  )}
                  onChange={setModel}
                  disabled={busy || restoring}
                />
              </div>
            </div>
          )}
          <Space className="intent-ai-actions">
            <Button onClick={newConversation}>{t('新会话')}</Button>
            {/* Deleting a session is destructive and irreversible, so it lives
              here behind a confirm rather than next to the send button. */}
            <Dropdown
              trigger={['click']}
              disabled={!sessionId || busy || restoring}
              menu={{
                items: [
                  {
                    key: 'delete',
                    danger: true,
                    icon: <DeleteOutlined />,
                    label: t('删除会话'),
                    onClick: () =>
                      modal.confirm({
                        title: t('删除这个会话？'),
                        content: t('会话记录会被移除，已执行的变更不受影响。'),
                        okText: t('删除会话'),
                        okButtonProps: { danger: true },
                        cancelText: t('取消'),
                        onOk: async () => {
                          await api.del(`/ai/sessions/${sessionId}`);
                          newConversation();
                          await refreshSessions();
                          message.success(t('会话已删除'));
                        },
                      }),
                  },
                ],
              }}
            >
              <Button
                icon={<MoreOutlined />}
                aria-label={t('会话操作')}
                disabled={!sessionId || busy || restoring}
              />
            </Dropdown>
          </Space>
        </div>
      }
    >
      {notice && (
        <Alert
          type="info"
          showIcon
          closable
          onClose={() => {
            markNoticeRead();
            setNotice(false);
          }}
          title={t('数据发送提示')}
          description={t(
            '会话内容和查询到的业务数据会发送给你配置的模型服务商。写操作会先展示计划与差异，确认后执行。',
          )}
          className="mb-4"
        />
      )}
      {!allowed ? (
        <Empty description={t('当前账号没有使用 AI 的权限')} />
      ) : (
        <>
          {restoring ? (
            <Loading panel tip={t('正在恢复会话与计划')} />
          ) : (
            <div
              ref={messageViewport}
              className="intent-ai-messages space-y-5"
              onWheel={() => {
                followOutput.current = false;
              }}
              onTouchMove={() => {
                followOutput.current = false;
              }}
              onScroll={(event) => {
                const viewport = event.currentTarget;
                followOutput.current =
                  viewport.scrollHeight -
                    viewport.scrollTop -
                    viewport.clientHeight <=
                  2;
              }}
            >
              {messages
                .filter(
                  (item) =>
                    item.role === 'user' || item.content || item.plan || busy,
                )
                .map((item) => (
                  <section
                    key={item.id}
                    className={
                      item.role === 'user'
                        ? 'intent-ai-message intent-ai-message-user'
                        : 'intent-ai-message intent-ai-message-assistant'
                    }
                  >
                    <div className="intent-markdown">
                      <ReactMarkdown
                        remarkPlugins={[remarkGfm]}
                        components={markdownComponents}
                      >
                        {item.content}
                      </ReactMarkdown>
                    </div>
                    {item.plan && (
                      <PlanCard
                        key={item.plan.id}
                        initialPlan={item.plan}
                        onNavigate={close}
                        onUpdated={(plan) =>
                          setMessages((old) =>
                            old.map((entry) =>
                              entry.id === item.id ? { ...entry, plan } : entry,
                            ),
                          )
                        }
                      />
                    )}
                    {item.planUnavailable && (
                      <Alert
                        type="warning"
                        title={t('关联计划暂时无法加载')}
                        action={
                          <Button
                            size="small"
                            onClick={() =>
                              history.push(
                                `/ai/sessions?planId=${encodeURIComponent(item.planId ?? '')}`,
                              )
                            }
                          >
                            {t('查看计划')}
                          </Button>
                        }
                      />
                    )}
                    {!item.navigated && !!item.evidence?.length && (
                      <PageSuggestions
                        evidence={item.evidence}
                        onNavigate={close}
                      />
                    )}
                  </section>
                ))}
              {busy && (
                <Space>
                  <Spin size="small" />
                  <Typography.Text type="secondary">
                    {tool || t('正在生成…')}
                  </Typography.Text>
                </Space>
              )}
            </div>
          )}
          {error && (
            <Alert type="error" showIcon title={error} className="my-3" />
          )}
          {/* One composer surface, matching the header command bar: the field
              and its actions read as a single control. */}
          <div
            className={`intent-ai-composer mt-4${composerFocus ? ' intent-ai-composer-focused' : ''}`}
          >
            <Input.TextArea
              ref={composer}
              value={text}
              disabled={restoring}
              variant="borderless"
              onChange={(event) => setText(event.target.value)}
              onFocus={() => setComposerFocus(true)}
              onBlur={() => setComposerFocus(false)}
              placeholder={t('描述你的目标，例如：停用选中的用户')}
              autoSize={{ minRows: 2, maxRows: 6 }}
              onPressEnter={(event) => {
                if (!event.shiftKey && !event.nativeEvent.isComposing) {
                  event.preventDefault();
                  void send(text);
                }
              }}
            />
            <div className="intent-ai-composer-actions">
              <span className="intent-ai-composer-hint">
                {t('Enter 发送 · Shift+Enter 换行')}
              </span>
              {busy && (
                <Button size="small" onClick={() => stopActive()}>
                  {t('停止')}
                </Button>
              )}
              <Button
                type="primary"
                size="small"
                disabled={!text.trim() || !models.length || restoring}
                loading={busy}
                onClick={() => void send(text)}
              >
                {t('发送')}
              </Button>
            </div>
          </div>
          {!models.length && (
            <Alert
              type="warning"
              className="mt-4"
              title={t('尚无可用模型')}
              description={t(
                '请在模型服务中配置并测试支持工具调用的模型后使用 AI。',
              )}
              action={
                <Button onClick={() => history.push('/system/ai-provider')}>
                  {t('模型服务')}
                </Button>
              }
            />
          )}
        </>
      )}
    </Drawer>
  );
}
