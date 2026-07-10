export type LocaleMessages = Record<string, unknown>

function isMessageObject(value: unknown): value is LocaleMessages {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

// Merge reconciliation additions without overwriting translations introduced by main.
export function mergeMissingLocaleMessages<T extends LocaleMessages, U extends LocaleMessages>(
  base: T,
  additions: U,
): T & U {
  const merged: LocaleMessages = { ...base }

  for (const [key, addition] of Object.entries(additions)) {
    if (!Object.prototype.hasOwnProperty.call(merged, key)) {
      merged[key] = addition
      continue
    }
    const current = merged[key]
    if (isMessageObject(current) && isMessageObject(addition)) {
      merged[key] = mergeMissingLocaleMessages(current, addition)
    }
  }

  return merged as T & U
}
