import { BellOutlined } from '@ant-design/icons';
import { history } from '@umijs/max';
import {
  App,
  Badge,
  Button,
  Empty,
  List,
  Popover,
  Space,
  Typography,
} from 'antd';
import { useEffect, useState } from 'react';
import { getLocale, localizeMessage, t } from '@/i18n';
import { useUserStore } from '@/stores/user';
import { ApiError, api, type PageData } from '@/utils/request';
import {
  exportUploadId,
  type Notice,
  type NoticeID,
  readNoticeIds,
  safeDownloadURL,
} from './notifications';
export function NotificationBell() {
  const identity = useUserStore((s) => s.identity);
  const userId = identity?.user.userId;
  const [notices, setNotices] = useState<Notice[]>([]);
  const [read, setRead] = useState<NoticeID[]>([]);
  const [open, setOpen] = useState(false);
  const { modal, message } = App.useApp();
  const marker = `intent-admin:notices:read:${identity?.user.userId ?? 'anonymous'}`;
  useEffect(() => {
    setRead(readNoticeIds(localStorage.getItem(marker)));
    setNotices([]);
    setOpen(false);
    if (!userId) return;
    let active = true;
    async function load() {
      try {
        const responses = await Promise.allSettled([
          api.get<PageData<Notice>>(
            '/notifications',
            { pageNum: 1, pageSize: 20, status: '0' },
            { silent: true },
          ),
          api.get<PageData<Notice>>(
            '/personal-notifications',
            { pageNum: 1, pageSize: 20 },
            { silent: true },
          ),
        ]);
        const rows = responses.flatMap((result) =>
          result.status === 'fulfilled' ? result.value.rows : [],
        );
        rows.sort(
          (a, b) =>
            Date.parse(b.createTime ?? '') - Date.parse(a.createTime ?? ''),
        );
        if (active) setNotices(rows.slice(0, 20));
      } catch {
        /* The authenticated request layer handles expired sessions. */
      }
    }
    void load();
    const visible = () => {
      if (!document.hidden) void load();
    };
    const timer = window.setInterval(visible, 30_000);
    window.addEventListener('focus', visible);
    return () => {
      active = false;
      window.clearInterval(timer);
      window.removeEventListener('focus', visible);
    };
  }, [userId, marker]);
  function mark(ids: NoticeID[]) {
    const next = [...new Set([...ids, ...read])].slice(0, 200);
    setRead(next);
    localStorage.setItem(marker, JSON.stringify(next));
    const personalIds = ids.filter(
      (id): id is string => typeof id === 'string',
    );
    if (personalIds.length)
      void api
        .post(
          '/personal-notifications/read',
          { ids: personalIds },
          { silent: true },
        )
        .catch(() => {});
  }
  async function show(notice: Notice) {
    mark([notice.noticeId]);
    setOpen(false);
    if (
      notice.link?.startsWith('/') &&
      !notice.link.startsWith('//') &&
      !notice.link.includes('\\')
    ) {
      history.push(notice.link);
      return;
    }
    const uploadId = exportUploadId(notice);
    if (uploadId) {
      try {
        const result = await api.get<{ url: string }>(
          `/system/upload/${encodeURIComponent(uploadId)}/url`,
          undefined,
          { dedupe: false },
        );
        const url = safeDownloadURL(result.url, window.location.origin);
        if (!url) throw new Error(t('下载地址无效，请联系管理员'));
        window.open(url, '_blank', 'noopener,noreferrer');
      } catch (error) {
        if (!(error instanceof ApiError))
          message.error(error instanceof Error ? error.message : t('下载失败'));
      }
      return;
    }
    modal.info({
      title: localizeMessage(notice.noticeTitle),
      width: 700,
      content: (
        <Typography.Paragraph className="whitespace-pre-wrap break-words">
          {notice.noticeContent}
        </Typography.Paragraph>
      ),
    });
  }
  if (!userId) return null;
  const unread = notices.filter(
    (notice) => !notice.readAt && !read.includes(notice.noticeId),
  );
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      trigger="click"
      placement="bottomRight"
      title={
        <Space className="w-full justify-between">
          <span>{t('通知')}</span>
          <Button
            type="link"
            size="small"
            onClick={() => mark(notices.map((notice) => notice.noticeId))}
          >
            {t('全部已读')}
          </Button>
        </Space>
      }
      content={
        <div className="max-h-96 w-80 overflow-auto">
          {notices.length ? (
            <List
              size="small"
              dataSource={notices}
              renderItem={(notice) => (
                <List.Item
                  className="cursor-pointer"
                  onClick={() => void show(notice)}
                >
                  <List.Item.Meta
                    title={
                      <Space>
                        <Badge
                          status={
                            notice.readAt || read.includes(notice.noticeId)
                              ? 'default'
                              : 'processing'
                          }
                        />
                        {notice.link
                          ? localizeMessage(notice.noticeTitle)
                          : notice.noticeTitle}
                        {exportUploadId(notice) && t(' · 下载')}
                      </Space>
                    }
                    description={
                      <Typography.Text type="secondary">
                        {notice.createTime
                          ? new Date(notice.createTime).toLocaleString(
                              getLocale(),
                            )
                          : ''}
                      </Typography.Text>
                    }
                  />
                </List.Item>
              )}
            />
          ) : (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={t('暂无通知')}
            />
          )}
        </div>
      }
    >
      <Button
        type="text"
        aria-label={t('通知，{value0} 条未读', { value0: unread.length })}
      >
        <Badge count={unread.length} size="small">
          <BellOutlined className="text-lg" />
        </Badge>
      </Button>
    </Popover>
  );
}
