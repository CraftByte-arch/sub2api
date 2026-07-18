import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PerformanceInvestigationDrawer from '@/views/admin/performance/components/PerformanceInvestigationDrawer.vue'
import { acquireModalBodyLock, lockAppInert, releaseAppInert, releaseModalBodyLock } from '../modalBodyLock'

afterEach(() => {
  releaseModalBodyLock()
  releaseModalBodyLock()
  releaseAppInert()
  releaseAppInert()
  document.body.style.overflow = ''
  document.getElementById('app')?.remove()
})

describe('modalBodyLock', () => {
  it('keeps the body locked until every consumer releases it', () => {
    document.body.style.overflow = 'scroll'

    acquireModalBodyLock()
    acquireModalBodyLock()
    expect(document.body.style.overflow).toBe('hidden')

    releaseModalBodyLock()
    expect(document.body.style.overflow).toBe('hidden')

    releaseModalBodyLock()
    expect(document.body.style.overflow).toBe('scroll')
  })

  it('restores a pre-existing inert attribute after the last release', () => {
    const appRoot = document.createElement('div')
    appRoot.id = 'app'
    appRoot.setAttribute('inert', 'pre-existing')
    document.body.append(appRoot)

    lockAppInert()
    lockAppInert()
    expect(appRoot.getAttribute('inert')).toBe('')

    releaseAppInert()
    expect(appRoot.getAttribute('inert')).toBe('')

    releaseAppInert()
    expect(appRoot.getAttribute('inert')).toBe('pre-existing')
  })

  it('keeps the app inert until overlapping BaseDialog and investigation drawer both close', async () => {
    const appRoot = document.createElement('div')
    appRoot.id = 'app'
    document.body.append(appRoot)
    const dialog = mount(BaseDialog, { props: { show: true, title: 'Shared dialog' } })
    const drawer = mount(PerformanceInvestigationDrawer, {
      props: {
        open: true,
        account: {
          account_id: 42,
          account_name: 'Shared dialog account',
          account_type: 'oauth',
          auth_mode: 'personalAccessToken',
          platform: 'openai',
          counters: {
            attempt_count: 1,
            success_count: 1,
            client_canceled_count: 0,
            ttft_timeout_count: 0,
            rate_limit_count: 0,
            auth_count: 0,
            upstream_4xx_count: 0,
            upstream_5xx_count: 0,
            transport_count: 0,
            protocol_count: 0,
            other_failure_count: 0,
            failover_count: 0,
            ttft_sum_ms: 0,
            duration_sum_ms: 0,
            ttft_latency: { Samples: 0, LE1000MS: 0, LE2500MS: 0, LE5000MS: 0, LE10000MS: 0, LE30000MS: 0, GT30000MS: 0 },
            duration_latency: { Samples: 0, LE1000MS: 0, LE2500MS: 0, LE5000MS: 0, LE10000MS: 0, LE30000MS: 0, GT30000MS: 0 }
          },
          availability: 1,
          failure_rate: 0,
          health_score: 1,
          p95_ttft_ms: 0,
          p95_duration_ms: 0,
          low_sample: false
        },
        investigation: null,
        loading: false,
        error: ''
      },
      global: {
        stubs: {
          PlatformTypeBadge: true,
          PerformanceMetricCard: true,
          PerformanceTrendChart: true,
          PerformanceFailureDistribution: true
        }
      }
    })
    await nextTick()

    expect(appRoot.hasAttribute('inert')).toBe(true)
    await drawer.setProps({ open: false })
    await nextTick()
    expect(appRoot.hasAttribute('inert')).toBe(true)

    await dialog.setProps({ show: false })
    await nextTick()
    expect(appRoot.hasAttribute('inert')).toBe(false)
    drawer.unmount()
    dialog.unmount()
  })
})
