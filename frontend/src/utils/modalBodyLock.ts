let lockCount = 0
let originalOverflow = ''

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
