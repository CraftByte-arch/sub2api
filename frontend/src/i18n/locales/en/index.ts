import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import featureAdditions from './featureAdditions'
import { mergeMissingLocaleMessages } from '../mergeMissing'

const messages = {
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  admin,
  ...misc,
}

export default mergeMissingLocaleMessages(messages, featureAdditions)
