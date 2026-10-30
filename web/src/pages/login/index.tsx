import {
  ArrowRightOutlined,
  CheckCircleOutlined,
  LockOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  ThunderboltOutlined,
  UserOutlined,
} from '@ant-design/icons';
import { useModel, useNavigate, useSearchParams } from '@umijs/max';
import {
  Alert,
  Button,
  Form,
  Input,
  message,
  Space,
  Spin,
  Typography,
} from 'antd';
import { useEffect, useState } from 'react';
import { t } from '@/i18n';
import LocaleSwitcher from '@/i18n/LocaleSwitcher';
import { safeRedirect } from '@/router/menu';
import { type LoginInput, resumeSession, useUserStore } from '@/stores/user';
import { api } from '@/utils/request';

interface Challenge {
  captchaEnabled: boolean;
  registerEnabled: boolean;
  uuid?: string;
  img?: string;
}

export default function LoginPage() {
  const navigate = useNavigate();
  const { setInitialState } = useModel('@@initialState');
  const [params] = useSearchParams();
  const [form] = Form.useForm<
    LoginInput & { nickName?: string; confirmPassword?: string }
  >();
  const [challenge, setChallenge] = useState<Challenge>();
  const [challengeError, setChallengeError] = useState(false);
  const [challengeLoading, setChallengeLoading] = useState(false);
  const [register, setRegister] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const signIn = useUserStore((s) => s.signIn);

  async function refreshChallenge() {
    setChallengeLoading(true);
    setChallengeError(false);
    form.setFieldValue('code', '');
    try {
      setChallenge(
        await api.get<Challenge>('/captchaImage', undefined, {
          ignoreUnauthorized: true,
          dedupe: false,
        }),
      );
    } catch {
      setChallenge(undefined);
      setChallengeError(true);
    } finally {
      setChallengeLoading(false);
    }
  }

  useEffect(() => {
    let active = true;
    async function initialize() {
      setChallengeLoading(true);
      const restored = await resumeSession();
      if (!active) return;
      if (restored) {
        navigate(safeRedirect(params.get('redirect')), { replace: true });
      } else {
        await refreshChallenge();
      }
    }
    void initialize();
    return () => {
      active = false;
    };
    // A challenge is fetched once per entry; subsequent attempts explicitly refresh it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function submit(values: LoginInput & { nickName?: string }) {
    if (!challenge) return;
    setSubmitting(true);
    const input = {
      username: values.username.trim(),
      password: values.password,
      code: values.code,
      uuid: challenge.uuid,
    };
    try {
      if (register) {
        await api.post(
          '/register',
          { ...input, nickName: values.nickName },
          {
            ignoreUnauthorized: true,
            dedupe: false,
          },
        );
        message.success(t('注册成功，请登录'));
        setRegister(false);
        form.setFieldsValue({ password: '', confirmPassword: '' });
        await refreshChallenge();
      } else {
        await signIn(input);
        await setInitialState((state) => ({
          ...state,
          identity: useUserStore.getState().identity,
          menus: useUserStore.getState().menus,
        }));
        navigate(safeRedirect(params.get('redirect')), { replace: true });
      }
    } catch {
      // The request layer reports the error. One-time captcha is consumed even on failure.
      await refreshChallenge();
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="intent-login">
      <header className="intent-login-header">
        <a href="/" className="intent-brand">
          <img src="/logo.svg" alt="" />
          <span>Intent</span>
        </a>
        <LocaleSwitcher />
      </header>
      <main className="intent-login-main">
        <section className="intent-login-intro">
          <div className="intent-login-eyebrow">
            <span />
            {t('统一管理工作空间')}
          </div>
          <h1>
            {t('让每个意图')}
            <br />
            {t('都有清晰的执行路径')}
          </h1>
          <p>
            {t(
              '连接日常管理与 AI 协作，从预览、确认到执行，让每一次变更都可追溯。',
            )}
          </p>
          <section
            className="intent-login-workflow"
            aria-label={t('Intent 执行流程')}
          >
            <div className="intent-workflow-step">
              <span className="intent-workflow-icon">
                <ThunderboltOutlined />
              </span>
              <div>
                <strong>{t('描述意图')}</strong>
                <span>{t('用自然语言表达管理目标')}</span>
              </div>
              <span className="intent-workflow-number">01</span>
            </div>
            <div className="intent-workflow-step">
              <span className="intent-workflow-icon intent-workflow-icon-purple">
                <CheckCircleOutlined />
              </span>
              <div>
                <strong>{t('预览确认')}</strong>
                <span>{t('执行前查看影响与变更内容')}</span>
              </div>
              <span className="intent-workflow-number">02</span>
            </div>
            <div className="intent-workflow-step">
              <span className="intent-workflow-icon intent-workflow-icon-green">
                <SafetyCertificateOutlined />
              </span>
              <div>
                <strong>{t('安全执行')}</strong>
                <span>{t('遵循权限边界，保留审计记录')}</span>
              </div>
              <span className="intent-workflow-number">03</span>
            </div>
          </section>
        </section>
        <section className="intent-login-panel">
          <div className="intent-login-heading">
            <div className="intent-login-panel-mark">
              <img src="/logo.svg" alt="" />
            </div>
            <Typography.Title level={2}>
              {register ? t('创建账号') : t('欢迎回来')}
            </Typography.Title>
            <Typography.Text type="secondary">
              {register
                ? t('填写以下信息，开启你的工作空间')
                : t('登录 Intent，继续你的管理工作')}
            </Typography.Text>
          </div>
          {challengeError && (
            <Alert
              type="error"
              showIcon
              title={t('加载登录配置失败')}
              className="mb-4"
              action={
                <Button size="small" onClick={() => void refreshChallenge()}>
                  {t('重试')}
                </Button>
              }
            />
          )}
          <Form
            form={form}
            name="login"
            layout="vertical"
            onFinish={(values) => void submit(values)}
            size="large"
          >
            <Form.Item
              name="username"
              label={t('账号')}
              rules={[
                { required: true, whitespace: true, message: t('请输入账号') },
                { max: 30, message: t('账号最多 30 个字符') },
              ]}
            >
              <Input
                prefix={<UserOutlined />}
                placeholder={t('账号')}
                autoComplete="username"
                maxLength={30}
              />
            </Form.Item>
            {register && (
              <Form.Item
                name="nickName"
                label={t('昵称')}
                rules={[{ max: 30, message: t('昵称最多 30 个字符') }]}
              >
                <Input placeholder={t('选填')} maxLength={30} />
              </Form.Item>
            )}
            <Form.Item
              name="password"
              label={t('密码')}
              rules={[
                { required: true, message: t('请输入密码') },
                ...(register
                  ? [{ min: 6, message: t('密码至少 6 个字符') }]
                  : []),
              ]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                placeholder={t('密码')}
                autoComplete={register ? 'new-password' : 'current-password'}
              />
            </Form.Item>
            {register && (
              <Form.Item
                name="confirmPassword"
                label={t('确认密码')}
                dependencies={['password']}
                rules={[
                  { required: true, message: t('请再次输入密码') },
                  ({ getFieldValue }) => ({
                    validator(_, value: string) {
                      return !value || getFieldValue('password') === value
                        ? Promise.resolve()
                        : Promise.reject(new Error(t('两次密码不一致')));
                    },
                  }),
                ]}
              >
                <Input.Password autoComplete="new-password" />
              </Form.Item>
            )}
            {challenge?.captchaEnabled && (
              <Form.Item label={t('验证码')} required>
                <Space.Compact className="w-full">
                  <Form.Item
                    name="code"
                    noStyle
                    rules={[{ required: true, message: t('请输入验证码') }]}
                  >
                    <Input
                      placeholder={t('图中字符')}
                      autoComplete="off"
                      maxLength={10}
                    />
                  </Form.Item>
                  <Button
                    className="h-10! px-1!"
                    aria-label={t('更换验证码')}
                    loading={challengeLoading}
                    onClick={() => void refreshChallenge()}
                  >
                    {challenge.img ? (
                      <img
                        src={
                          challenge.img.startsWith('data:')
                            ? challenge.img
                            : `data:image/png;base64,${challenge.img}`
                        }
                        alt={t('登录验证码')}
                        className="h-9 w-28"
                      />
                    ) : (
                      <ReloadOutlined />
                    )}
                  </Button>
                </Space.Compact>
              </Form.Item>
            )}
            {!challenge && challengeLoading && (
              <div className="mb-4 text-center">
                <Spin />
              </div>
            )}
            <Button
              className="intent-login-submit"
              type="primary"
              htmlType="submit"
              block
              loading={submitting}
              disabled={!challenge || challengeLoading}
            >
              {register ? t('创建账号') : t('登录')}
              <ArrowRightOutlined />
            </Button>
            {challenge?.registerEnabled && (
              <Button
                type="link"
                block
                className="mt-3"
                onClick={() => {
                  setRegister(!register);
                  form.setFieldsValue({
                    password: '',
                    confirmPassword: '',
                    code: '',
                  });
                  void refreshChallenge();
                }}
              >
                {register ? t('已有账号，返回登录') : t('创建新账号')}
              </Button>
            )}
          </Form>
          <div className="intent-login-panel-footer">
            <SafetyCertificateOutlined />
            {t('权限控制 · 操作审计 · 变更追踪')}
          </div>
        </section>
      </main>
      <footer className="intent-login-footer">Intent · Ant Design Pro</footer>
    </div>
  );
}
