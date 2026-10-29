import { useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Space,
  Spin,
  Typography,
  message,
} from 'antd';
import {
  LockOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
  UserOutlined,
} from '@ant-design/icons';
import { useNavigate, useSearchParams, useModel } from '@umijs/max';

import { safeRedirect } from '@/router/menu';
import { resumeSession, useUserStore, type LoginInput } from '@/stores/user';
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
        message.success('注册成功，请登录');
        setRegister(false);
        form.setFieldsValue({ password: '', confirmPassword: '' });
        await refreshChallenge();
      } else {
        await signIn(input);
        await setInitialState((state) => ({ ...state, identity: useUserStore.getState().identity, menus: useUserStore.getState().menus }));
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
    <div className="flex min-h-full items-center justify-center bg-slate-50 p-6">
      <Card className="w-full max-w-md shadow-sm">
        <div className="mb-8 text-center">
          <ThunderboltOutlined className="mb-3 text-4xl text-blue-600" />
          <Typography.Title level={2} className="mb-1!">
            Intent Admin
          </Typography.Title>
          <Typography.Text type="secondary">
            {register ? '创建账号' : '登录管理控制台'}
          </Typography.Text>
        </div>
        {challengeError && (
          <Alert
            type="error"
            title="加载登录配置失败"
            className="mb-4"
            action={
              <Button size="small" onClick={() => void refreshChallenge()}>
                重试
              </Button>
            }
          />
        )}
        <Form
          form={form}
          layout="vertical"
          onFinish={(values) => void submit(values)}
          size="large"
        >
          <Form.Item
            name="username"
            label="账号"
            rules={[
              { required: true, whitespace: true, message: '请输入账号' },
              { max: 30, message: '账号最多 30 个字符' },
            ]}
          >
            <Input
              prefix={<UserOutlined />}
              placeholder="账号"
              autoComplete="username"
              maxLength={30}
            />
          </Form.Item>
          {register && (
            <Form.Item
              name="nickName"
              label="昵称"
              rules={[{ max: 30, message: '昵称最多 30 个字符' }]}
            >
              <Input placeholder="选填" maxLength={30} />
            </Form.Item>
          )}
          <Form.Item
            name="password"
            label="密码"
            rules={[
              { required: true, message: '请输入密码' },
              ...(register ? [{ min: 6, message: '密码至少 6 个字符' }] : []),
            ]}
          >
            <Input.Password
              prefix={<LockOutlined />}
              placeholder="密码"
              autoComplete={register ? 'new-password' : 'current-password'}
            />
          </Form.Item>
          {register && (
            <Form.Item
              name="confirmPassword"
              label="确认密码"
              dependencies={['password']}
              rules={[
                { required: true, message: '请再次输入密码' },
                ({ getFieldValue }) => ({
                  validator(_, value: string) {
                    return !value || getFieldValue('password') === value
                      ? Promise.resolve()
                      : Promise.reject(new Error('两次密码不一致'));
                  },
                }),
              ]}
            >
              <Input.Password autoComplete="new-password" />
            </Form.Item>
          )}
          {challenge?.captchaEnabled && (
            <Form.Item label="验证码" required>
              <Space.Compact className="w-full">
                <Form.Item
                  name="code"
                  noStyle
                  rules={[{ required: true, message: '请输入验证码' }]}
                >
                  <Input
                    placeholder="图中字符"
                    autoComplete="off"
                    maxLength={10}
                  />
                </Form.Item>
                <Button
                  className="h-10! px-1!"
                  aria-label="更换验证码"
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
                      alt="登录验证码"
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
            type="primary"
            htmlType="submit"
            block
            loading={submitting}
            disabled={!challenge || challengeLoading}
          >
            {register ? '注册' : '登录'}
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
              {register ? '已有账号，返回登录' : '创建新账号'}
            </Button>
          )}
        </Form>
      </Card>
    </div>
  );
}
