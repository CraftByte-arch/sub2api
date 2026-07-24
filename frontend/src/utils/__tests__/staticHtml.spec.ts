import { describe, expect, it } from 'vitest'

import { buildStaticHtmlDocument, hasVisibleStaticHtml } from '@/utils/staticHtml'

function parseBuiltDocument(source: string): Document {
  return new DOMParser().parseFromString(buildStaticHtmlDocument(source), 'text/html')
}

describe('buildStaticHtmlDocument', () => {
  it('marks the sandbox document with the resolved theme', () => {
    expect(parseBuiltDocument('<p>Hello</p>').documentElement.dataset.theme).toBe('light')
    expect(
      new DOMParser()
        .parseFromString(buildStaticHtmlDocument('<p>Hello</p>', 'dark'), 'text/html')
        .documentElement.dataset.theme,
    ).toBe('dark')
  })

  it('keeps static layout while removing active content', () => {
    const doc = parseBuiltDocument(`
      <!doctype html>
      <html>
        <head>
          <title>About</title>
          <style>h1 { color: red; }</style>
          <meta http-equiv="refresh" content="0;url=https://example.com">
        </head>
        <body>
          <h1 onclick="alert(1)">Hello</h1>
          <script>alert(1)</script>
          <form action="https://example.com"><input name="secret"><button>Send</button></form>
          <iframe src="https://example.com"></iframe>
          <object data="https://example.com"></object>
          <svg onload="alert(1)"><circle /></svg>
          <img src="data:image/png;base64,AA==" onerror="alert(1)">
        </body>
      </html>
    `)

    expect(doc.title).toBe('About')
    expect(doc.querySelector('style')?.textContent).toContain('color: red')
    expect(doc.querySelector('h1')?.textContent).toBe('Hello')
    expect(doc.querySelector('h1')?.hasAttribute('onclick')).toBe(false)
    expect(doc.querySelector('script, form, input, button, iframe, object, svg')).toBeNull()
    expect(doc.querySelector('img')?.hasAttribute('onerror')).toBe(false)
    expect(doc.querySelector('meta[http-equiv="refresh"]')).toBeNull()

    const csp = doc.querySelector<HTMLMetaElement>('meta[http-equiv="Content-Security-Policy"]')
    expect(csp?.content).toContain("default-src 'none'")
    expect(csp?.content).toContain("script-src 'none'")
    expect(csp?.content).toContain("form-action 'none'")
  })

  it('normalizes supported links and disables relative files', () => {
    const doc = parseBuiltDocument(`
      <a id="fragment" href="#part" target="_blank">Fragment</a>
      <a id="app" href="/orders">App</a>
      <a id="external" href="https://example.com/docs" ping="https://tracker.example">External</a>
      <a id="mail" href="mailto:help@example.com">Mail</a>
      <a id="phone" href="tel:+123456">Phone</a>
      <a id="relative" href="other.html">Relative</a>
      <a id="protocol-relative" href="//example.com">Protocol relative</a>
      <a id="bad" href="javascript:alert(1)">Bad</a>
    `)

    expect(doc.querySelector('#fragment')?.getAttribute('target')).toBeNull()
    expect(doc.querySelector('#app')?.getAttribute('target')).toBe('_top')
    expect(doc.querySelector('#external')?.getAttribute('target')).toBe('_blank')
    expect(doc.querySelector('#external')?.getAttribute('rel')).toBe('noopener noreferrer')
    expect(doc.querySelector('#external')?.hasAttribute('ping')).toBe(false)
    expect(doc.querySelector('#mail')?.getAttribute('target')).toBe('_blank')
    expect(doc.querySelector('#phone')?.getAttribute('target')).toBe('_blank')
    expect(doc.querySelector('#relative')?.hasAttribute('href')).toBe(false)
    expect(doc.querySelector('#protocol-relative')?.hasAttribute('href')).toBe(false)
    expect(doc.querySelector('#bad')?.hasAttribute('href')).toBe(false)
  })
})

describe('hasVisibleStaticHtml', () => {
  it('accepts text and static media but rejects empty or active-only source', () => {
    expect(hasVisibleStaticHtml('<p>Hello</p>')).toBe(true)
    expect(hasVisibleStaticHtml('<img src="data:image/png;base64,AA==">')).toBe(true)
    expect(hasVisibleStaticHtml('<table><tr><td></td></tr></table>')).toBe(true)
    expect(hasVisibleStaticHtml('  <style>body { color: red; }</style>  ')).toBe(false)
    expect(hasVisibleStaticHtml('<script>alert(1)</script>')).toBe(false)
  })
})
