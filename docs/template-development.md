# Business page components

Use the existing shared CRUD page and ordinary React components. No generator or
department permission model is required. `demo/item` demonstrates create/edit,
version checking, details, date filtering, import/export and private attachments.

## Fields

A field configuration can be reused by `CrudPage` or by `Fields` inside a custom
Ant Design form. Use `t` for authored labels. This example shows a master/detail
order form; its business capability owns validation and persistence of `lines`.
The template does not create an order table or expose arbitrary nested writes.

```tsx
const fields: Field[] = [
  { name: 'customerId', label: t('客户'), type: 'remote', required: true,
    remote: { url: '/sales/customer/list', queryKey: 'name',
      valueKey: 'customerId', labelKey: 'name',
      resolveUrl: '/sales/customer/:id' } },
  { name: 'createdRange', label: t('创建时间'), type: 'dateRange',
    search: true, form: false, column: false,
    rangeKeys: ['beginTime', 'endTime'] },
  { name: 'lines', label: t('明细'), type: 'items', maxCount: 100,
    fields: [
      { name: 'name', label: t('名称'), required: true },
      { name: 'quantity', label: t('数量'), type: 'number', required: true,
        rules: [{ type: 'number', min: 1 }] },
      { name: 'unitPrice', label: t('单价'), type: 'number', required: true,
        rules: [{ type: 'number', min: 0 }] },
    ] },
];
```

Remote selectors debounce searches and ignore stale responses. A `resolveUrl`
loads the label of an existing selected record outside the current search page.
Search endpoints return either an array or `{rows: [...]}`. Detail endpoints
return one record. IDs keep their original string/number type.

Date ranges use two ISO timestamps. `rangeKeys` maps them to the business
endpoint's begin/end parameters for both list and export requests. Empty ranges
clear both parameters. Detail responses convert date values back to Day.js
instances for editors. The URL retains filters for page navigation.

`type: 'file'` and `type: 'image'` are generic upload fields returning an array of
opaque upload IDs, with `maxCount` limiting the form field. They use ordinary
file-management permissions; a business write must validate ownership and bind
those IDs itself. `image` restricts selection to PNG/JPEG/WebP and the backend
still validates the actual format. For confidential record attachments, use the
private business attachment integration below instead of generic upload fields.
Structured line items and upload IDs are excluded from AI form drafts; users
edit them in the browser. Their final submission uses the normal capability.

## Details and private record attachments

```tsx
<CrudPage title={t('订单')} base="/sales/order" idKey="orderId"
  perm="sales:order" detail attachments={{ entity: 'sales.order' }}
  fields={fields} />
```

`detail` adds a permission-controlled drawer using the ordinary detail endpoint.
`detailPerm` can override the default `sales:order:query`. When an existing record
is edited or opened in details, the attachment component lists its private
files. Edits require the business edit permission; read-only details can download
without file-management permissions. Save a new record before adding attachments.
Attachment edits are independent actions and become visible immediately.

Register the server policy once during bootstrap:

```go
resourcesService.RegisterAttachmentPolicy("sales.order", resources.AttachmentPolicy{
    Authorize: func(ctx context.Context, recordID string, write bool) error {
        // Resolve the authenticated actor; validate the ID; query the current
        // order through dbx.DBFrom(ctx, db); check its live read/write policy.
        // Return an error for a missing/deleted record or a denied principal.
        return orders.AuthorizeAttachment(ctx, recordID, write)
    },
})
```

Only registered entity names are accepted. Uploads use
`POST /api/system/attachment/upload?entity=sales.order&recordId=123`.
`GET /api/system/attachment/list` returns file metadata and a revision.
`PUT /api/system/attachment` replaces up to 20 upload IDs and requires that
revision. It rejects another user's uploads, files claimed by another record,
and stale edits. Removing a link does not physically delete the file.
All downloads stream through authenticated handlers and recheck the record.
File-manager grants and upload ownership do not bypass a bound record policy.

## Version-aware writes and retries

The detail/list response of a versioned entity includes `version`. Shared CRUD
captures it when opening the editor, sends it with updates, and preserves the
form on conflict. Custom services compare it with their current locked row or
use an atomic `WHERE id = ? AND version = ?` update and check RowsAffected.
Use `concurrency.Check` for a consistent 409 error; install a version-increment
trigger for each new versioned table. Legacy clients can omit version, so new
editors should always include it.

For a transactional, credential-free write result, set `Meta.Deduplicate = true`.
Send a fresh `Idempotency-Key` per user intent, and reuse it only to retry the
same request after an ambiguous outcome. Keys are principal/source/token and
capability bound. The 24-hour receipt commits atomically with the write; payload
changes return 409, failed transactions can retry, and live permission/risk
checks still precede receipt replay. The shared action hook manages keys in
memory and does not automatically retry writes.
