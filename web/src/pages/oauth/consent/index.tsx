import { SafetyCertificateOutlined } from '@ant-design/icons';
import { PageContainer } from '@ant-design/pro-components';
import { Link, Navigate, useLocation } from '@umijs/max';
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Descriptions,
  Result,
  Space,
  Spin,
  Typography,
} from 'antd';
import { useEffect, useRef, useState } from 'react';
import { getLocale, t } from '@/i18n';
import { api, auth } from '@/utils/request';
import {
  type ConsentScope,
  consentRedirect,
  consentRequestId,
  requestedScopes,
} from './helpers';

interface ConsentRequest {
  requestId: string;
  clientName: string;
  redirectUri: string;
  scopes: ConsentScope[];
  expiresAt: string;
}

export default function OAuthConsentPage() {
  const location = useLocation();
  const requestId = consentRequestId(location.search);
  const [request, setRequest] = useState<ConsentRequest>();
  const [selected, setSelected] = useState<string[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [now, setNow] = useState(Date.now());
  const pending = useRef(false);
  const token = auth.get();

  useEffect(() => {
    let active = true;
    setError('');
    setRequest(undefined);
    if (!requestId || !token) {
      setLoading(false);
      return;
    }
    setLoading(true);
    void api
      .get<ConsentRequest>(
        '/oauth/consent',
        { request_id: requestId },
        { silent: true, dedupe: false },
      )
      .then((data) => {
        if (!active) return;
        if (
          data.requestId !== requestId ||
          !consentRedirect(data.redirectUri) ||
          !Number.isFinite(Date.parse(data.expiresAt))
        )
          throw new Error(t('授权请求无效，请从客户端重新发起授权'));
        setRequest(data);
        setSelected(data.scopes.map((scope) => scope.name));
      })
      .catch((reason: unknown) => {
        if (active)
          setError(
            reason instanceof Error ? reason.message : t('加载授权请求失败'),
          );
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [requestId, token]);

  useEffect(() => {
    if (!request) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [request]);

  if (!token)
    return (
      <Navigate
        to={`/login?redirect=${encodeURIComponent(location.pathname + location.search)}`}
        replace
      />
    );
  const expired = request ? Date.parse(request.expiresAt) <= now : false;

  async function decide(approve: boolean) {
    if (!request || expired || pending.current) return;
    pending.current = true;
    setSubmitting(true);
    setError('');
    try {
      const result = await api.post<{ redirectUri: string }>(
        '/oauth/consent',
        {
          requestId: request.requestId,
          approve,
          scopes: approve ? requestedScopes(selected, request.scopes) : [],
        },
        { silent: true, dedupe: false },
      );
      const redirect = consentRedirect(result.redirectUri);
      if (!redirect) throw new Error(t('客户端回调地址无效，请重新发起授权'));
      window.location.assign(redirect.href);
    } catch (reason) {
      setError(
        reason instanceof Error ? reason.message : t('提交授权决定失败'),
      );
      pending.current = false;
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen bg-slate-50 p-4 md:p-10">
      <PageContainer
        title={t('授权 MCP 客户端')}
        subTitle={t('Intent 账号授权')}
        ghost
      >
        <Card className="mx-auto max-w-2xl">
          {loading ? (
            <div className="py-16 text-center">
              <Spin size="large" />
            </div>
          ) : !requestId || !request ? (
            <Result
              status="warning"
              title={t('授权请求不可用')}
              subTitle={error || t('请从 MCP 客户端重新发起授权。')}
              extra={<Link to="/">{t('返回控制台')}</Link>}
            />
          ) : (
            <Space orientation="vertical" size="large" className="w-full">
              <Space align="start">
                <SafetyCertificateOutlined className="mt-1 text-3xl text-blue-600" />
                <div>
                  <Typography.Title level={3} className="mb-1!">
                    {request.clientName} {t(' 请求访问你的账号')}
                  </Typography.Title>
                  <Typography.Text type="secondary">
                    {t(
                      '确认后，该客户端只能在下列授权范围及你当前账号权限内访问 MCP 服务。',
                    )}
                  </Typography.Text>
                </div>
              </Space>
              <Descriptions
                column={1}
                size="small"
                bordered
                items={[
                  {
                    key: 'client',
                    label: t('客户端'),
                    children: request.clientName,
                  },
                  {
                    key: 'redirect',
                    label: t('回调地址'),
                    children: (
                      <Typography.Text className="break-all">
                        {request.redirectUri}
                      </Typography.Text>
                    ),
                  },
                  {
                    key: 'expires',
                    label: t('请求有效期至'),
                    children: new Date(request.expiresAt).toLocaleString(
                      getLocale(),
                    ),
                  },
                ]}
              />
              <div>
                <Typography.Title level={5}>
                  {t('选择授权范围')}
                </Typography.Title>
                <Checkbox.Group
                  value={selected}
                  onChange={(values) => setSelected(values.map(String))}
                  disabled={submitting || expired}
                >
                  <Space orientation="vertical">
                    {request.scopes.map((scope) => (
                      <Checkbox key={scope.name} value={scope.name}>
                        <strong>{scope.name}</strong> — {t(scope.description)}
                      </Checkbox>
                    ))}
                  </Space>
                </Checkbox.Group>
              </div>
              <Alert
                type="info"
                showIcon
                title={t('授权后仍会检查当前账号权限')}
                description={t(
                  '写操作需要预览和明确确认。你可以拒绝本次授权；拒绝后将返回客户端。',
                )}
              />
              {expired && (
                <Alert
                  type="warning"
                  showIcon
                  title={t('授权请求已过期')}
                  description={t('请返回 MCP 客户端重新发起授权。')}
                />
              )}
              {error && <Alert type="error" showIcon title={error} />}
              <Space wrap>
                <Button
                  type="primary"
                  loading={submitting}
                  disabled={expired || selected.length === 0}
                  onClick={() => void decide(true)}
                >
                  {t('同意授权')}
                </Button>
                <Button
                  disabled={submitting || expired}
                  onClick={() => void decide(false)}
                >
                  {t('拒绝')}
                </Button>
                <Link to="/">{t('返回控制台')}</Link>
              </Space>
            </Space>
          )}
        </Card>
      </PageContainer>
    </div>
  );
}
