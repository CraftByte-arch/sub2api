import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import featureAdditions from './featureAdditions'
import { mergeMissingLocaleMessages } from '../mergeMissing'

const messages = {
  ...landing,
  ...common,
  ...dashboard,
  ...batchImage,
  admin,
  ...misc,
}

export default mergeMissingLocaleMessages(messages, featureAdditions)
