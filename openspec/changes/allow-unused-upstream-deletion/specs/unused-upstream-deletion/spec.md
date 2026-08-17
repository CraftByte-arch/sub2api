## ADDED Requirements

### Requirement: Only persisted upstreams with no matching local API Key accounts SHALL be deletable
The sidecar SHALL authorize upstream deletion only for a persisted record whose normalized API address is not currently used by any local API Key account, and SHALL determine that eligibility from a fresh server-side account listing.

#### Scenario: Persisted upstream has no local API Key accounts
- **WHEN** an authenticated administrator deletes a persisted upstream and the fresh local account list has no API Key account with the same normalized API address
- **THEN** the sidecar deletes the persisted upstream record and returns HTTP 204

#### Scenario: Address has become active
- **WHEN** an authenticated administrator attempts deletion after a local API Key account begins using the normalized address
- **THEN** the sidecar returns HTTP 409 with `UPSTREAM_IN_USE` and leaves the complete persisted record unchanged

#### Scenario: Current account state cannot be verified
- **WHEN** the sidecar cannot retrieve current local accounts while processing deletion
- **THEN** the operation fails without deleting the upstream record

### Requirement: Deletion SHALL affect only sidecar upstream state
Successful deletion SHALL remove the persisted upstream record including identities, encrypted credentials, recharge rate, balance and Key snapshots, and stored bindings, and SHALL NOT delete, disable, edit, or otherwise mutate a Sub2API account.

#### Scenario: Unused upstream with saved management data is deleted
- **WHEN** the administrator confirms deletion of an eligible record containing identities, rate configuration, balances, and remote-Key snapshots
- **THEN** those values are absent from sidecar state and no Sub2API account mutation endpoint is called

### Requirement: Reused addresses SHALL automatically reappear as fresh candidates
The sidecar SHALL continue deriving upstream candidates from current local API Key account addresses without retaining deletion tombstones.

#### Scenario: No account uses the address after deletion
- **WHEN** an eligible persisted upstream is deleted and no local API Key account uses that address
- **THEN** the upstream is absent from the next upstream list

#### Scenario: A later account uses the deleted address
- **WHEN** a local API Key account later uses the same normalized API address
- **THEN** the upstream automatically reappears on the next list refresh as an unpersisted candidate with no restored identity, credential, rate, balance, or Key snapshot data

### Requirement: The administrator UI SHALL require explicit destructive confirmation
The upstream list SHALL show a visible danger-styled deletion action only when the row is persisted and its local-account list is empty, and SHALL require a second-confirmation dialog before issuing the administrator DELETE request.

#### Scenario: Row displays the empty local-account state
- **WHEN** a persisted upstream renders “没有使用该地址的本地 API Key 账号”
- **THEN** the row also renders an accessible “删除上游” action

#### Scenario: Row has at least one local API Key account
- **WHEN** an upstream lists one or more local API Key accounts
- **THEN** no upstream deletion action is rendered

#### Scenario: Administrator confirms deletion
- **WHEN** the confirmation dialog is open and the administrator selects “确认删除”
- **THEN** the button becomes disabled with progress feedback, duplicate submission is prevented, the list reloads after completion, and a success or sanitized failure message is shown

#### Scenario: Administrator cancels deletion
- **WHEN** the administrator closes or cancels the confirmation dialog
- **THEN** no DELETE request is sent and keyboard focus returns to the triggering control when it still exists

### Requirement: Upstream deletion SHALL require administrator authentication
The deletion endpoint SHALL use the same administrator authentication middleware as all other upstream-management operations.

#### Scenario: Unauthenticated caller attempts deletion
- **WHEN** a request without a valid administrator session calls `DELETE /api/upstreams/{upstreamID}`
- **THEN** the sidecar returns an authentication error without listing accounts or deleting state
