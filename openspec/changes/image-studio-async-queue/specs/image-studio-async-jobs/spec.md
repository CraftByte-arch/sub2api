## ADDED Requirements

### Requirement: Site-authenticated image studio jobs are submitted asynchronously
The system SHALL provide site-authenticated endpoints for logged-in image studio users to submit asynchronous image generation jobs without changing the behavior of existing public synchronous image generation APIs.

#### Scenario: Submit text-to-image job
- **WHEN** an authenticated logged-in image studio user submits a valid text-to-image request
- **THEN** the system creates a persisted job in `queued` state
- **AND** the system returns a job identifier instead of waiting for image generation to finish

#### Scenario: Submit image-to-image job
- **WHEN** an authenticated logged-in image studio user submits a valid image-to-image request with required reference files
- **THEN** the system creates a persisted job in `queued` state
- **AND** the system stores enough request metadata to execute the job later

#### Scenario: Existing public image endpoints remain unchanged
- **WHEN** an external API client calls an existing public synchronous image generation endpoint
- **THEN** the request is handled by the existing synchronous flow
- **AND** no site-authenticated asynchronous job is created implicitly

### Requirement: Jobs are scheduled with per-user FIFO and global concurrency control
The system SHALL execute image studio jobs using per-user FIFO ordering and a configurable global concurrency limit.

#### Scenario: Same user jobs preserve submission order
- **WHEN** the same user has multiple queued jobs
- **THEN** only that user's earliest queued job is eligible to run first
- **AND** later jobs for the same user remain queued until the earlier job leaves `queued` or `running`

#### Scenario: Global concurrency limit is enforced
- **WHEN** the number of running image studio jobs reaches the configured global maximum
- **THEN** additional eligible jobs remain in `queued` state
- **AND** no new job starts until a running job finishes or fails

#### Scenario: Stale running job is recovered
- **WHEN** a running job no longer updates its heartbeat within the configured timeout window
- **THEN** the system marks that running execution as stale
- **AND** the job becomes eligible for recovery according to the worker recovery policy

### Requirement: Image studio jobs use pre-charge billing with failure refund
The system SHALL reserve the user's charge at submission time, finalize it on success, and refund it on failure.

#### Scenario: Submission reserves cost
- **WHEN** a logged-in image studio user submits a job with sufficient available balance
- **THEN** the system creates a billing hold for the estimated job cost before the job is queued

#### Scenario: Failure refunds reserved cost
- **WHEN** a queued or running image studio job ends in `failed`
- **THEN** the system releases or refunds the reserved billing hold exactly once
- **AND** the job records the failure reason

#### Scenario: Success finalizes reserved cost
- **WHEN** an image studio job ends in `succeeded`
- **THEN** the system finalizes the reserved billing hold exactly once
- **AND** the user is not charged a second time for the same job

### Requirement: Successful jobs expose thumbnails in lists and originals on demand
The system SHALL store one original image and one thumbnail per successful job, show thumbnails in job listings, and provide original images only when requested by detail views.

#### Scenario: Job list returns thumbnail metadata
- **WHEN** a logged-in image studio user requests the job list
- **THEN** the system returns each job's summary status and thumbnail metadata if assets are available
- **AND** the system does not require the client to download the original image for list rendering

#### Scenario: Job detail returns original image access
- **WHEN** a logged-in image studio user requests a successful job's detail
- **THEN** the system returns metadata required to fetch the original image
- **AND** the original image is not eagerly included in job list payloads

#### Scenario: Failed job has no image assets
- **WHEN** a job finishes in `failed`
- **THEN** the system does not expose thumbnail or original image assets for that job

### Requirement: Image assets expire according to global retention settings
The system SHALL support a global administrator-configured retention duration for image studio assets using hour or day units, with `0` meaning no automatic expiration.

#### Scenario: Successful job gets computed expiration
- **WHEN** a job succeeds and the global retention setting is greater than zero
- **THEN** the system computes and stores an `expires_at` timestamp for that job's assets

#### Scenario: Zero retention disables expiration
- **WHEN** the global retention value is set to `0`
- **THEN** successful jobs do not receive an automatic expiration timestamp for asset cleanup

#### Scenario: Expired assets are deleted but job record remains
- **WHEN** a successful job's assets reach `expires_at`
- **THEN** the system deletes the local original and thumbnail files
- **AND** the system retains the job record, billing history, and non-file metadata
- **AND** the system marks the asset state as deleted

#### Scenario: Expired job detail reflects deleted assets
- **WHEN** a logged-in image studio user requests a job whose assets were deleted by retention cleanup
- **THEN** the system reports that the job succeeded historically
- **AND** the system indicates that the image assets are no longer available
