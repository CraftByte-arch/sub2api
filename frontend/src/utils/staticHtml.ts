import DOMPurify from 'dompurify'

export const STATIC_HTML_SANDBOX =
  'allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation'

const STATIC_HTML_CSP = [
  "default-src 'none'",
  "script-src 'none'",
  "style-src 'unsafe-inline'",
  'img-src http: https: data:',
  'media-src http: https: data:',
  'font-src http: https: data:',
  "connect-src 'none'",
  "frame-src 'none'",
  "object-src 'none'",
  "form-action 'none'",
  "base-uri 'none'"
].join('; ')

const FORBIDDEN_TAGS = [
  'script',
  'iframe',
  'object',
  'embed',
  'base',
  'meta',
  'link',
  'form',
  'input',
  'button',
  'select',
  'textarea',
  'option',
  'svg',
  'math'
]

function normalizeStaticLinks(doc: Document): void {
  doc.querySelectorAll<HTMLAnchorElement>('a').forEach((anchor) => {
    anchor.removeAttribute('ping')
    anchor.removeAttribute('download')

    const href = anchor.getAttribute('href')?.trim() || ''
    anchor.removeAttribute('target')
    anchor.removeAttribute('rel')

    if (href.startsWith('#')) return
    if (href.startsWith('/') && !href.startsWith('//')) {
      anchor.target = '_top'
      return
    }

    try {
      const parsed = new URL(href)
      if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
        anchor.target = '_blank'
        anchor.rel = 'noopener noreferrer'
        return
      }
      if (parsed.protocol === 'mailto:' || parsed.protocol === 'tel:') {
        anchor.target = '_blank'
        anchor.rel = 'noopener noreferrer'
        return
      }
    } catch {
      // Relative and malformed links are made inert below.
    }

    anchor.removeAttribute('href')
  })
}

export function buildStaticHtmlDocument(source: string): string {
  const sanitized = DOMPurify.sanitize(source, {
    WHOLE_DOCUMENT: true,
    ADD_TAGS: ['style'],
    FORBID_TAGS: FORBIDDEN_TAGS,
    FORBID_ATTR: ['srcdoc', 'action', 'formaction', 'srcset'],
    ALLOW_DATA_ATTR: false
  })
  const doc = new DOMParser().parseFromString(String(sanitized), 'text/html')
  normalizeStaticLinks(doc)

  const csp = doc.createElement('meta')
  csp.httpEquiv = 'Content-Security-Policy'
  csp.content = STATIC_HTML_CSP
  doc.head.prepend(csp)

  return `<!doctype html>\n${doc.documentElement.outerHTML}`
}

export function hasVisibleStaticHtml(source: string): boolean {
  const doc = new DOMParser().parseFromString(buildStaticHtmlDocument(source), 'text/html')
  const body = doc.body.cloneNode(true) as HTMLElement
  body.querySelectorAll('style').forEach((style) => style.remove())
  return Boolean(
    body.textContent?.trim() || body.querySelector('img, audio, video, table, hr')
  )
}
