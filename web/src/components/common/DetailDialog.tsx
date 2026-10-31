import { Modal, type ModalProps } from 'antd';
import type { ReactNode } from 'react';

type DetailDialogProps = Omit<ModalProps, 'onCancel' | 'footer'> & {
  onClose: () => void;
  extra?: ReactNode;
};

/** A centered detail surface with independent scrolling and optional header actions. */
export function DetailDialog({
  title,
  extra,
  onClose,
  className,
  children,
  ...props
}: DetailDialogProps) {
  return (
    <Modal
      {...props}
      centered
      className={['intent-detail-modal', className].filter(Boolean).join(' ')}
      title={
        <div className="intent-detail-modal-title">
          <span>{title}</span>
          {extra && <div className="intent-detail-modal-actions">{extra}</div>}
        </div>
      }
      footer={null}
      onCancel={onClose}
    >
      {children}
    </Modal>
  );
}
