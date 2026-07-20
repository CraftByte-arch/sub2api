import overview from './overview'
import channels from './channels'
import accounts from './accounts'
import resources from './resources'
import ops from './ops'
import settings from './settings'
import ttft from './ttft'
import audit from './audit'
import promptAudit from './promptAudit'
import monitoring from './monitoring'

export default {
  ...overview,
  ...channels,
  ...accounts,
  ...resources,
  ...ops,
  ...settings,
  ...ttft,
  ...audit,
  ...promptAudit,
  ...monitoring,
}
