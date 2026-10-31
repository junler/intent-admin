import { Spin, Typography } from 'antd';

/**
 * A centred loading state.
 *
 * `<Spin />` on its own is an inline element, so dropping one into a page,
 * drawer or modal parks it in the top-left corner rather than where the
 * content is about to appear. Use this instead.
 *
 * `panel` sizes it for a drawer or modal body; the default fills a page.
 */
export function Loading({ tip, panel }: { tip?: string; panel?: boolean }) {
  return (
    <div
      className={
        panel ? 'intent-loading intent-loading-panel' : 'intent-loading'
      }
    >
      <Spin size="large" />
      {tip && <Typography.Text type="secondary">{tip}</Typography.Text>}
    </div>
  );
}
