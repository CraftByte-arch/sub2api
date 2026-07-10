import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import admin from './admin'
import misc from './misc'
import featureAdditions from './featureAdditions'
import { mergeMissingLocaleMessages } from '../mergeMissing'

const messages = {
  ...landing,
  ...common,
  ...dashboard,
  admin,
  ...misc,
}

export default mergeMissingLocaleMessages(messages, featureAdditions)
