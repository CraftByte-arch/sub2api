export default {
  monitoring: {
    title: 'Monitoring',
    description: 'A unified view of request health, first-byte latency, and account-level anomalies',
    coverage: 'Coverage {start} to {end}',
    health: {
      complete: 'Collection healthy',
      degraded: 'Collection degraded',
      pending: 'Awaiting collection'
    },
    degradedBanner: 'The collector is degraded; metrics may be incomplete. {dropped} samples dropped, {pending} pending.',
    protection: {
      enabled: 'First token protection · {seconds}s',
      disabled: 'First token protection off',
      adjust: 'Adjust'
    },
    filters: {
      range: 'Time range',
      platform: 'Platform',
      allPlatforms: 'All platforms',
      model: 'Model',
      modelPlaceholder: 'Filter by model'
    },
    kpi: {
      availability: 'Availability',
      failureRate: 'Failure rate',
      ttftTimeoutRate: 'TTFT timeout rate',
      recoveryRate: 'Failover recovery rate',
      p95Ttft: 'P95 TTFT',
      requestsContext: '{count} requests',
      ratioContext: '{numerator} / {denominator}',
      timeoutsContext: '{count} timeouts',
      p95TtftContext: 'P50 {p50} · duration {duration}',
      trendAria: '{label} trend',
      trendSummary: '{label} trend: {direction} {delta}',
      trendUp: 'rising',
      trendDown: 'falling'
    },
    funnel: {
      title: 'First token protection path',
      subtitle: 'Failover recovery and final outcomes after timeouts',
      controlled: 'Controlled requests',
      triggered: 'Timeout triggered',
      recovered: 'Recovered via failover',
      finalFailure: 'Final failure',
      platformNote: 'Funnel data is not affected by the platform filter',
      summarySeparator: ', '
    },
    trends: {
      rates: 'Request health trend',
      latency: 'Latency trend',
      availability: 'Availability',
      failureRate: 'Failure rate',
      ttftTimeoutRate: 'TTFT timeout rate',
      p50Ttft: 'P50 TTFT',
      p95Ttft: 'P95 TTFT',
      p95Duration: 'P95 duration',
      empty: 'No data for the selected range'
    },
    accounts: {
      title: 'Account performance',
      total: '{count} accounts',
      searchPlaceholder: 'Search account name or ID',
      account: 'Account',
      platform: 'Platform',
      status: 'Status',
      availability: 'Availability',
      failureRate: 'Failure rate',
      ttftTimeoutRate: 'TTFT timeout rate',
      p95Ttft: 'P95 TTFT',
      samples: 'Samples',
      healthy: 'Healthy',
      watch: 'Watch',
      risk: 'At risk',
      lowSample: 'Low sample',
      empty: 'No account performance data for the selected range',
      loading: 'Loading account performance',
      retry: 'Retry',
      viewDetails: 'View performance details for account {name}'
    },
    failures: {
      title: 'Failure distribution',
      empty: 'No failures recorded',
      countLabel: 'Failures',
      tooltipCount: '{count}',
      loading: 'Loading',
      outcomes: {
        ttft_timeout: 'TTFT timeout',
        rate_limit: 'Rate limit',
        auth: 'Auth',
        upstream_4xx: 'Upstream 4xx',
        upstream_5xx: 'Upstream 5xx',
        transport: 'Transport',
        protocol: 'Protocol',
        other_failure: 'Other'
      }
    },
    drawer: {
      title: 'Account performance details',
      loading: 'Loading account details',
      empty: 'No performance data to analyze',
      availability: 'Availability',
      failureRate: 'Failure rate',
      p95Ttft: 'P95 TTFT',
      p95Duration: 'P95 duration',
      successContext: '{success} / {total} succeeded',
      failureContext: '{failure} / {total} failed',
      ttftContext: 'Time to first byte',
      durationContext: 'Full request duration',
      trendTitle: 'Performance trend',
      failureTitle: 'Failure distribution'
    },
    settings: {
      title: 'First token timeout protection',
      description: 'Automatically retries with another account when the first token takes longer than the configured seconds.',
      enabled: 'Enable protection',
      timeoutSeconds: 'Timeout (seconds)',
      timeoutError: 'Timeout must be an integer between 1 and 300',
      effectiveEnabled: 'Effective: {seconds}s',
      effectiveDisabled: 'Currently disabled'
    },
    empty: {
      title: 'No samples yet',
      description: 'Performance samples accumulate after deployment once requests are processed.'
    },
    errors: {
      overview: 'Failed to load monitoring overview',
      accounts: 'Failed to load account performance data',
      investigation: 'Failed to load account details',
      settings: 'Failed to save settings'
    }
  }
}
