let lockCount = 0
let originalOverflow = ''
let inertLockCount = 0
let inertAppRoot: (HTMLElement & { inert?: boolean }) | null = null
let originalInertAttribute: string | null = null

export function acquireModalBodyLock() {
  if (lockCount === 0) {
    originalOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
  }
  lockCount += 1
}

export function releaseModalBodyLock() {
  if (lockCount === 0) return

  lockCount -= 1
  if (lockCount === 0) document.body.style.overflow = originalOverflow
}

export function lockAppInert() {
  if (inertLockCount === 0) {
    inertAppRoot = document.getElementById('app') as (HTMLElement & { inert?: boolean }) | null
    if (!inertAppRoot) return false
    originalInertAttribute = inertAppRoot.getAttribute('inert')
  }
  if (!inertAppRoot) return false

  inertLockCount += 1
  inertAppRoot.setAttribute('inert', '')
  inertAppRoot.inert = true
  return true
}

export function releaseAppInert() {
  if (inertLockCount === 0 || !inertAppRoot) return

  inertLockCount -= 1
  if (inertLockCount !== 0) return

  if (originalInertAttribute === null) {
    inertAppRoot.removeAttribute('inert')
    inertAppRoot.inert = false
  } else {
    inertAppRoot.setAttribute('inert', originalInertAttribute)
    inertAppRoot.inert = true
  }
  inertAppRoot = null
  originalInertAttribute = null
}
