import admin from '../i18n/messages/admin.json';
import changesets from '../i18n/messages/changesets.json';
import common from '../i18n/messages/common.json';
import errors from '../i18n/messages/errors.json';
import mcp from '../i18n/messages/mcp.json';
import metadata from '../i18n/messages/metadata.json';
import monitor from '../i18n/messages/monitor.json';
import plans from '../i18n/messages/plans.json';
import system from '../i18n/messages/system.json';

const messages: Record<string, string> = {
  ...errors,
  ...metadata,
  ...admin,
  ...system,
  ...plans,
  ...changesets,
  ...mcp,
  ...monitor,
  ...common,
};
export default messages;
