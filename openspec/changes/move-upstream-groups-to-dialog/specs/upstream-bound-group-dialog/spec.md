## ADDED Requirements

### Requirement: Keep upstream details compact
The upstream-management list SHALL not render the complete available-group table inside each expanded login identity. Each upstream row SHALL provide a labeled control that opens its upstream-group view and displays the number of available upstream groups.

#### Scenario: An upstream has available group snapshots
- **WHEN** one or more available-group snapshots exist under an upstream
- **THEN** the upstream row displays an enabled upstream-group control with the number of available groups

#### Scenario: An upstream has no available group snapshots
- **WHEN** no available-group snapshot exists under an upstream
- **THEN** the upstream row still provides the upstream-group control and its view explains that no group snapshot exists

### Requirement: Show upstream groups and bindings in a modal dialog
The sidecar SHALL open a modal dialog that groups all available upstream groups by login identity. Each group row SHALL show the group name, platform type, group multiplier, final multiplier, and any bound remote Key names and local API Key accounts. An available group without a local API Key binding SHALL state that it is unbound.

#### Scenario: A group snapshot has a bound remote key
- **WHEN** a bound remote key's group matches an available-group snapshot for the same login identity
- **THEN** the dialog displays the snapshot's platform type and calculated multiplier values together with the bound remote Key and local API Key account

#### Scenario: A group snapshot has no bound remote key
- **WHEN** no remote key in an available group is bound to a local API Key account
- **THEN** the dialog retains that group row and explicitly identifies it as unbound

#### Scenario: A bound remote key has no matching group snapshot
- **WHEN** a bound remote key has a group value that does not match the identity's available-group snapshot
- **THEN** the dialog displays an inferred group row and clearly marks the snapshot as unavailable without omitting the binding

### Requirement: Preserve accessible modal interaction
The bound-group dialog SHALL have a descriptive title, a visible close control, Escape cancellation, and focus restoration to the triggering upstream-row control. The dialog SHALL remain readable without horizontal scrolling at narrow viewport widths.

#### Scenario: Closing the bound-group dialog
- **WHEN** an administrator closes the dialog through its close control or Escape
- **THEN** focus returns to the button that opened the dialog
