-- Migration: 197_channel_monitor_stream_mode
-- Channel monitor probes use streaming responses by default. Existing rows are
-- initialized to true so their first post-upgrade check follows the same
-- default as newly created monitors.

ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS stream BOOLEAN NOT NULL DEFAULT TRUE;
