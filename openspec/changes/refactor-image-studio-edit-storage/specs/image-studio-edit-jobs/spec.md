## ADDED Requirements

### Requirement: Multipart edit job upload
The system SHALL create Image Studio edit jobs from multipart requests containing between one and four ordered reference image files and at most one mask file.

#### Scenario: Submit one compressed reference image
- **WHEN** a user submits an edit job with one frontend-compressed WebP `image` file and valid edit metadata
- **THEN** the system accepts the multipart request and creates one queued edit job

#### Scenario: Submit four compressed reference images
- **WHEN** a user submits four `image` file fields in a defined order
- **THEN** the system accepts all four files and preserves their order for task execution

#### Scenario: Reject a fifth reference image
- **WHEN** an edit job request contains more than four `image` file fields
- **THEN** the system rejects the request and removes every file staged for that request

#### Scenario: Reject an edit job without a reference image
- **WHEN** an edit job request contains no valid `image` file
- **THEN** the system rejects the request without creating a queued task

#### Scenario: Preserve mask fidelity
- **WHEN** a user includes a mask file with an edit job
- **THEN** the frontend uploads the mask without lossy compression and the backend stores at most one validated mask file

### Requirement: Frontend reference image compression
The Image Studio frontend SHALL convert each selected reference image to a WebP file at quality 0.72 while preserving its pixel dimensions before uploading the binary file.

#### Scenario: Compress four images before upload
- **WHEN** a user selects four supported reference images and submits an edit job
- **THEN** the frontend uploads four compressed WebP files rather than data URLs or the original uncompressed files

#### Scenario: Compression failure stops submission
- **WHEN** any selected reference image cannot be decoded or converted to WebP
- **THEN** the frontend reports the compression error and does not submit a partial edit job

### Requirement: Server-managed input file persistence
The system SHALL persist edit input files under the configured Image Studio data directory and SHALL store only ordered relative paths and lifecycle metadata in the database.

#### Scenario: Persist files before queueing
- **WHEN** all uploaded files pass validation
- **THEN** the system atomically finalizes the files and creates the queued job with their relative paths

#### Scenario: Database creation fails after staging
- **WHEN** input files were staged but the job record cannot be created
- **THEN** the system removes the entire staged input directory

#### Scenario: Database contains no image bytes
- **WHEN** an edit job is created through the new upload flow
- **THEN** its request payload contains no data URL, base64 image, `images`, or `mask` binary content

#### Scenario: Reject unsafe paths
- **WHEN** a stored or derived input path is absolute, traverses with `..`, or resolves outside the Image Studio root
- **THEN** the system refuses to read or delete that path and fails the task with a storage validation error

### Requirement: Edit input validation
The system MUST validate each reference image and mask independently by file count, bounded size, detected MIME type, and decodable image content.

#### Scenario: Reject oversized input
- **WHEN** any uploaded reference image or mask exceeds the configured per-file limit
- **THEN** the system rejects the full request and removes all staged files

#### Scenario: Reject spoofed image MIME
- **WHEN** an uploaded file declares an image MIME type but its content is not a supported decodable image
- **THEN** the system rejects the full request

#### Scenario: Reject an incompatible mask
- **WHEN** an uploaded mask does not preserve supported transparent image content or its pixel dimensions differ from the first reference image
- **THEN** the system rejects the full request and removes all staged files

### Requirement: Input storage availability
The system SHALL isolate Image Studio input-storage failure from unrelated API traffic while preventing tasks from being accepted or claimed without durable shared storage.

#### Scenario: Storage probe fails
- **WHEN** the service cannot create, read, and delete a probe file under the configured Image Studio input root
- **THEN** new Image Studio asynchronous jobs receive a clear 503 response and the Worker pauses task claims while unrelated APIs remain available

### Requirement: Protocol-correct asynchronous execution
The asynchronous Worker SHALL reconstruct the upstream edit request from stored files using the protocol required by the selected account.

#### Scenario: Execute through an API Key account
- **WHEN** an edit job selects an OpenAI API Key account
- **THEN** the Worker sends multipart `/v1/images/edits` with ordered repeated `image` file parts, an optional `mask` file part, and the edit metadata fields

#### Scenario: Clean API Key multipart spool
- **WHEN** an API Key edit attempt completes or aborts
- **THEN** its temporary multipart spool file is removed, with stale spool files eligible for bounded orphan cleanup

#### Scenario: Execute through an OAuth account
- **WHEN** an edit job selects an OpenAI OAuth account
- **THEN** the Worker converts the stored files to the existing Responses image edit representation without writing image bytes to the database

#### Scenario: Input file is missing or corrupt
- **WHEN** a Worker cannot safely read or decode any stored input file
- **THEN** the job enters a terminal input storage failure state without repeatedly calling the upstream provider

### Requirement: Input file lifecycle
The system SHALL retain input files only while they are needed for execution or retry and SHALL delete them after durable upstream completion, expiration, or user deletion.

#### Scenario: Successful job deletes inputs
- **WHEN** the upstream result, output assets, and settlement recovery payload have been durably persisted
- **THEN** the system deletes all reference images and the mask and records the input deletion time

#### Scenario: Retryable failure retains inputs
- **WHEN** an edit attempt fails with a retryable error before the input expiration time
- **THEN** the system retains the input files for the next attempt

#### Scenario: Queued input expires
- **WHEN** a queued edit job passes its input expiration time before execution
- **THEN** the system atomically prevents the job from becoming running, deletes the input files, and marks the job failed with an `input_expired` error

#### Scenario: Input TTL is independent from output retention
- **WHEN** an edit job is created without an explicit administrative override
- **THEN** its input expires after the independent 24-hour default while generated outputs continue to use the existing output retention setting

#### Scenario: Running input reaches expiration
- **WHEN** an edit job reaches its input expiration time while a Worker is actively processing it
- **THEN** the cleanup process does not delete its files mid-execution and the Worker deletes them after leaving the running execution path

#### Scenario: User deletes a task
- **WHEN** a user deletes an Image Studio edit job
- **THEN** the system idempotently removes its input files, output files, and database record

#### Scenario: Orphan input directory expires
- **WHEN** an input directory is older than the cleanup grace period and no database job references it
- **THEN** the cleanup process deletes the orphan directory

### Requirement: Legacy edit task migration
The system SHALL remove embedded image content from terminal legacy jobs and SHALL provide a bounded compatibility path for active legacy edit jobs.

#### Scenario: Clean terminal legacy payloads
- **WHEN** the schema migration encounters a terminal edit job containing `images` or `mask` data URLs
- **THEN** it removes those fields while retaining non-binary task metadata and history

#### Scenario: Materialize an active legacy task
- **WHEN** a Worker claims an active legacy edit job with valid data URLs and no stored input paths
- **THEN** it writes up to four ordered reference images and the optional mask to managed storage, updates the path fields, removes the embedded image fields, and continues execution

#### Scenario: Reject invalid legacy cardinality
- **WHEN** an active legacy task has zero or more than four reference images
- **THEN** the system marks it terminally invalid and removes embedded image content that can be safely cleared
