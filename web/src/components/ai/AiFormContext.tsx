import { createContext, type ReactNode, useContext } from 'react';
import type { FormFocus } from './panelState';

const FormContext = createContext<FormFocus | undefined>(undefined);

/** Form metadata contains identities and field names only, never input values. */
export function AiFormScope({
  value,
  children,
}: {
  value?: FormFocus;
  children: ReactNode;
}) {
  return (
    <FormContext.Provider value={value}>
      <div
        style={{ display: 'contents' }}
        data-ai-form={value ? JSON.stringify(value) : undefined}
      >
        {children}
      </div>
    </FormContext.Provider>
  );
}

export function AiFormMarker({
  form,
  field,
}: {
  form?: FormFocus;
  field?: string;
  label?: string;
  compact?: boolean;
}) {
  const scope = useContext(FormContext);
  const target = form ?? scope;
  return target ? (
    <span
      hidden
      data-ai-form={JSON.stringify({ ...target, ...(field ? { field } : {}) })}
    />
  ) : null;
}

export function AiFormHeading({
  title,
  form,
}: {
  title: ReactNode;
  form: FormFocus;
}) {
  return <span data-ai-form={JSON.stringify(form)}>{title}</span>;
}
