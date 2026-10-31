import { t } from '@/i18n';
import { formPrompt } from './formPrompt';
import type { FormFocus } from './panelState';

export interface AssistantPopup {
  title: string;
  layer: number;
  forms: FormFocus[];
  hasInputs: boolean;
}

/** A topmost business dialog wins over page context, including an older editor. */
export function chooseAssistantContext(
  route: string,
  pageTitle: string,
  popups: AssistantPopup[],
  inlineForms: FormFocus[] = [],
) {
  const popup = popups
    .map((value, order) => ({ ...value, order }))
    .sort((a, b) => b.layer - a.layer || b.order - a.order)[0];
  const layer = Math.max(1800, (popup?.layer ?? 0) + 100);
  let form: FormFocus | undefined = popup?.forms[0];
  if (!popup)
    form = inlineForms.find((item) => item.mode !== 'action' || !!item.path);
  if (!form && popup?.hasInputs) {
    form = {
      title: popup.title || pageTitle,
      mode: /新增|添加|创建|\b(new|create|add)\b/i.test(popup.title)
        ? 'create'
        : /编辑|修改|更新|\b(edit|update)\b/i.test(popup.title)
          ? 'update'
          : 'action',
    };
  }
  if (form) {
    const target = { ...form, route: form.route ?? route };
    return { form: target, prompt: formPrompt(target), layer };
  }
  return {
    form: undefined,
    prompt: popup
      ? t('我正在查看“{title}”，请帮我：', { title: popup.title || pageTitle })
      : t('我正在“{page}”页面，请帮我：', { page: pageTitle }),
    layer,
  };
}

function visible(element: Element): boolean {
  if (element.closest('[hidden], [aria-hidden="true"]')) return false;
  const style = window.getComputedStyle(element);
  return (
    style.display !== 'none' &&
    style.visibility !== 'hidden' &&
    element.getClientRects().length > 0
  );
}

function readForms(container: ParentNode, inlineOnly = false): FormFocus[] {
  return Array.from(container.querySelectorAll('[data-ai-form]')).flatMap(
    (marker) => {
      if (inlineOnly && marker.closest('.ant-modal-wrap, .ant-drawer'))
        return [];
      // A metadata marker can be hidden; visibility belongs to its actual form or dialog.
      const form = marker.closest('form') ?? marker.querySelector('form');
      const surface =
        form ??
        marker.closest('.ant-modal-wrap, .ant-drawer') ??
        marker.parentElement;
      if (!surface || !visible(surface)) return [];
      try {
        const value = JSON.parse(
          marker.getAttribute('data-ai-form') ?? 'null',
        ) as FormFocus | null;
        if (
          !value ||
          typeof value.title !== 'string' ||
          !['create', 'update', 'action'].includes(value.mode)
        )
          return [];
        return [value];
      } catch {
        return [];
      }
    },
  );
}

/** Observe labels and metadata only. Input values, passwords and keys are never inspected. */
export function detectAssistantContext(route: string, pageTitle: string) {
  const popups = Array.from(
    document.querySelectorAll('.ant-modal-wrap, .ant-drawer'),
  )
    .filter(
      (popup) =>
        visible(popup) &&
        !popup.querySelector('.intent-ai-drawer') &&
        !popup.classList.contains('intent-ai-drawer'),
    )
    .filter(
      (popup) =>
        !popup.classList.contains('ant-drawer') ||
        popup.classList.contains('ant-drawer-open'),
    )
    .map((popup) => ({
      title:
        popup
          .querySelector('.ant-modal-title, .ant-drawer-title')
          ?.textContent?.trim() ?? '',
      layer: Number.parseInt(window.getComputedStyle(popup).zIndex, 10) || 1000,
      forms: readForms(popup),
      hasInputs: !!popup.querySelector(
        'form, input, textarea, [role="combobox"]',
      ),
    }));
  return chooseAssistantContext(
    route,
    pageTitle,
    popups,
    readForms(document, true),
  );
}
