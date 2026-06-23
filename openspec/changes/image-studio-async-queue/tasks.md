## 1. Backend job model and settings

- [ ] 1.1 Add persistent image studio job schema, status fields, asset metadata, retention metadata, and billing hold linkage.
- [ ] 1.2 Add global admin settings for image studio retention value/unit and global async concurrency.
- [ ] 1.3 Add repository and service primitives for job creation, status updates, stale-job recovery, and cleanup scanning.

## 2. Worker execution and billing

- [ ] 2.1 Implement backend worker polling with per-user FIFO candidate selection and global concurrency enforcement.
- [ ] 2.2 Implement job execution flow for text-to-image and image-to-image using existing image generation services through internal service boundaries.
- [ ] 2.3 Implement pre-charge hold creation, success finalization, failure refund, and idempotent recovery behavior.

## 3. Asset storage and retention

- [ ] 3.1 Implement local original image storage and thumbnail generation for successful jobs.
- [ ] 3.2 Add backend asset access endpoints for thumbnails and originals with resource-state checks.
- [ ] 3.3 Implement scheduled cleanup for expired local assets while preserving job records and metadata.

## 4. Backend API surface

- [ ] 4.1 Add site-authenticated async image studio job submit endpoint without changing public synchronous image endpoints.
- [ ] 4.2 Add paginated job list and job detail endpoints that separate thumbnail summaries from original image access.
- [ ] 4.3 Add API validation, permission checks, and failure responses for unsupported payloads or insufficient balance.

## 5. Frontend image studio integration

- [ ] 5.1 Refactor image studio submission flow to create async jobs and poll backend job list/detail endpoints.
- [ ] 5.2 Replace browser-only history with backend-backed job history that shows thumbnails and asset-expired states.
- [ ] 5.3 Cache original images on the frontend for preview reuse and download without refetching when still valid.
