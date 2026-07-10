import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

function localeValue(locale: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((value, key) => {
    if (!value || typeof value !== 'object') return undefined
    return (value as Record<string, unknown>)[key]
  }, locale)
}

const requiredKeys = [
  'nav.imageStudio',
  'nav.myUsageCards',
  'nav.usageCardManagement',
  'usageCards.status.active',
  'keys.billingPriority.options.usageCardOnly',
  'usage.deductionSources.usageCard',
  'imageStudio.title',
  'imageStudio.description',
  'admin.settings.tabs.imageStudio',
  'admin.settings.imageStudio.title',
  'admin.settings.imageStudio.description',
  'admin.settings.imageStudio.asyncConcurrency',
  'admin.settings.imageStudio.asyncConcurrencyHint',
  'admin.settings.imageStudio.retention',
  'admin.settings.imageStudio.retentionHint',
  'admin.settings.imageStudio.hours',
  'admin.settings.imageStudio.days',
  'admin.settings.imageStudio.availableGroups',
  'admin.settings.imageStudio.availableGroupsHint',
  'admin.settings.imageStudio.availableGroupsEmpty',
  'admin.settings.imageStudio.toolDeclarationPolicy',
  'admin.settings.imageStudio.toolDeclarationPolicyHint',
  'admin.settings.imageStudio.toolDeclarationPolicyStrip',
  'admin.settings.imageStudio.toolDeclarationPolicyAllow',
  'admin.settings.imageStudio.toolDeclarationPolicyReject',
  'admin.usageCards.validation.nameRequired',
  'admin.users.usageCardPurchased',
  'admin.groups.form.usageCardDisabled',
  'admin.channelMonitor.form.endpointModeSelf',
  'admin.accounts.openai.imageProtocolPreference',
  'admin.redeem.types.usage_card',
  'admin.settings.features.openaiLongContextBilling.title',
  'admin.settings.defaults.defaultUsageCards',
  'admin.settings.site.usageCardBillingEnabled',
  'admin.settings.payment.merchantOrderPrefix',
  'payment.admin.usageCardOrder',
]

describe.each([
  ['en', en],
  ['zh', zh],
])('%s Image Studio locale', (_name, locale) => {
  it.each(requiredKeys)('defines %s', (key) => {
    expect(localeValue(locale, key), key).toEqual(expect.any(String))
  })
})
