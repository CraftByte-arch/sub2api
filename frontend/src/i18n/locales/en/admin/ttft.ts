export default {
  ttft: {
    title: 'First Token Monitoring',
    description: 'Monitor first-token timeout failover and account-level outcomes.',
    settings: {
      title: 'First Token Timeout Policy',
      description: 'The policy applies to eligible streaming requests started after saving.',
      enabled: 'Enable first token timeout',
      timeoutSeconds: 'Timeout (seconds)',
      timeoutError: 'Enter a whole number from 1 to 300.',
      effectiveEnabled: 'Effective: enabled ({seconds}s)',
      effectiveDisabled: 'Effective: disabled',
      loadedAt: 'Loaded {time}'
    },
    filters: { range: 'Range', protocol: 'Protocol', model: 'Model' },
    metrics: {
      controlledRequests: 'Controlled requests',
      clientCanceled: '{count} client canceled',
      attemptTimeout: 'Attempt TTFT timeout rate',
      recovery: 'Recovery rate',
      finalTTFTFailure: 'Final TTFT failure rate',
      otherFinalFailure: 'Other final failure rate'
    },
    charts: { failureTrend: 'Hourly failure rates', failureDistribution: 'Other failure distribution' },
    completeness: { degraded: 'Statistics are degraded: {dropped} dropped samples, {pending} pending samples.', lastSuccessfulFlush: 'Last successful flush: {time}.', noSuccessfulFlush: 'No successful flush has completed yet.' },
    accounts: {
      title: 'Account statistics', account: 'Account / platform', accountId: 'Account ID', accountIdError: 'Enter a positive whole account ID.', platform: 'Platform', pageSize: 'Page size',
      samples: 'Non-canceled attempt samples', success: 'Success', ttft: 'TTFT timeout', other: 'Other failure', avgTTFT: 'Average TTFT',
      lowSample: 'Low sample (<20)', previous: 'Previous', next: 'Next'
    },
    errors: { settings: 'Unable to save or load the timeout policy.', overview: 'Unable to load first token statistics.', accounts: 'Unable to load account statistics.' },
    empty: 'No statistics are available for this selection.'
  }
}
