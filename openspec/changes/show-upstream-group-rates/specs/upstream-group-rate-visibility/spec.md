## ADDED Requirements

### Requirement: Persist available upstream group snapshots per identity
The sidecar SHALL persist all available upstream groups returned during a successful identity synchronization, separately for each upstream login identity. Each snapshot SHALL include a stable group identifier, display name, normalized platform type when supplied by the upstream, group multiplier when supplied by the upstream, multiplier source, synchronization time, and stale status.

#### Scenario: A successful Sub2API group synchronization
- **WHEN** a Sub2API identity synchronization receives the available groups and group rates
- **THEN** the sidecar stores every returned available group for that identity and uses the user-specific rate when one is returned for the group

#### Scenario: A group snapshot cannot be refreshed
- **WHEN** an upstream identity synchronization cannot retrieve its group information but has an existing group snapshot
- **THEN** the sidecar retains the existing groups and marks their snapshot stale rather than replacing it with an empty list

### Requirement: Expose group rates and final rates in the upstream management view
The sidecar SHALL return each identity's available group snapshots in the existing upstream-management response. For every group that has both a group multiplier and an upstream recharge rate, the response SHALL include the final multiplier calculated as recharge rate multiplied by group multiplier.

#### Scenario: A final multiplier is calculable
- **WHEN** an identity has a recharge rate and an available group has a numerical multiplier
- **THEN** the upstream-management response includes the calculated final multiplier for that group

#### Scenario: A final multiplier is not calculable
- **WHEN** either the recharge rate or the group multiplier is unavailable
- **THEN** the response leaves the final multiplier unset and does not substitute zero

### Requirement: Display available group details in upstream management
The upstream-management interface SHALL display all available groups under the relevant login identity, including group name, platform type, group multiplier, final multiplier, and synchronization freshness. The interface SHALL display OpenAI, Anthropic, Gemini, and GROK as distinct labeled types when explicitly provided by the upstream and SHALL display an explicit unknown-type label otherwise.

#### Scenario: An identity exposes multiple platform groups
- **WHEN** an administrator expands an upstream identity with groups of different reported platforms
- **THEN** each group row displays its own labeled platform type, original multiplier, and final multiplier

#### Scenario: An upstream does not report a platform or multiplier
- **WHEN** a returned group has no recognized platform or has no numerical multiplier
- **THEN** the interface shows “未知类型” and “未提供” respectively without presenting a false numerical value
