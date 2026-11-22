-- Private, immutable workbench files. Existing image objects are adopted by
-- metadata/pins only: no copy, rename, public ACL or second image quota.
CREATE TABLE office_resources (
 id UUID PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 source VARCHAR(16) NOT NULL CHECK(source IN ('upload','generated','gallery')),
 gallery_image_id VARCHAR(128),
 latest_version_id UUID,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 deleted_at TIMESTAMPTZ,
 UNIQUE(user_id,id),
 UNIQUE(user_id,gallery_image_id)
);
CREATE TABLE office_resource_versions (
 id UUID PRIMARY KEY,
 user_id BIGINT NOT NULL,
 resource_id UUID NOT NULL,
 parent_version_id UUID,
 run_id UUID,
 publication_id VARCHAR(128) NOT NULL,
 name VARCHAR(240) NOT NULL,
 mime VARCHAR(128) NOT NULL,
 byte_size BIGINT NOT NULL CHECK(byte_size>0 AND byte_size<=52428800),
 sha256 CHAR(64) NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 state VARCHAR(16) NOT NULL CHECK(state IN ('pending','ready','deleting')),
 storage_endpoint TEXT NOT NULL,
 storage_region TEXT NOT NULL,
 storage_bucket TEXT NOT NULL,
 object_key TEXT NOT NULL,
 preview_key TEXT NOT NULL DEFAULT '',
 preview_mime VARCHAR(128) NOT NULL DEFAULT '',
 preview_bytes BIGINT NOT NULL DEFAULT 0 CHECK(preview_bytes>=0 AND preview_bytes<=8388608),
 preview_sha256 VARCHAR(64) NOT NULL DEFAULT '',
 preview_kind VARCHAR(16) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(user_id,id),
 UNIQUE(user_id,resource_id,id),
 UNIQUE(user_id,publication_id),
 FOREIGN KEY(user_id,resource_id) REFERENCES office_resources(user_id,id) ON DELETE CASCADE,
 FOREIGN KEY(user_id,run_id) REFERENCES office_agent_runs(user_id,id) ON DELETE CASCADE,
 FOREIGN KEY(user_id,resource_id,parent_version_id) REFERENCES office_resource_versions(user_id,resource_id,id)
);
CREATE INDEX idx_office_resources_page ON office_resources(user_id,created_at DESC,id) WHERE deleted_at IS NULL;
CREATE INDEX idx_office_versions_cleanup ON office_resource_versions(state,updated_at);
CREATE TABLE office_resource_refs (
 user_id BIGINT NOT NULL,
 version_id UUID NOT NULL,
 conversation_id UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(user_id,version_id,conversation_id),
 FOREIGN KEY(user_id,version_id) REFERENCES office_resource_versions(user_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(user_id,conversation_id) REFERENCES office_agent_conversations(user_id,id) ON DELETE CASCADE
);
CREATE TABLE office_gallery_pins (
 user_id BIGINT NOT NULL,
 image_id VARCHAR(128) NOT NULL,
 resource_id UUID NOT NULL,
 PRIMARY KEY(user_id,image_id),
 FOREIGN KEY(user_id,resource_id) REFERENCES office_resources(user_id,id) ON DELETE CASCADE,
 FOREIGN KEY(user_id,image_id) REFERENCES image_studio_gallery_assets(user_id,image_id) ON DELETE RESTRICT
);
CREATE TABLE office_workspace_snapshots (
 id UUID PRIMARY KEY,
 user_id BIGINT NOT NULL,
 run_id UUID NOT NULL,
 operation_id VARCHAR(128) NOT NULL,
 version BIGINT NOT NULL CHECK(version>0),
 sha256 CHAR(64) NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 byte_size BIGINT NOT NULL CHECK(byte_size>0 AND byte_size<=41943040),
 state VARCHAR(16) NOT NULL CHECK(state IN ('pending','ready','deleting')),
 storage_endpoint TEXT NOT NULL,
 storage_region TEXT NOT NULL,
 storage_bucket TEXT NOT NULL,
 object_key TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(run_id,operation_id),
 UNIQUE(run_id,version),
 FOREIGN KEY(user_id,run_id) REFERENCES office_agent_runs(user_id,id) ON DELETE CASCADE
);
CREATE INDEX idx_office_snapshots_cleanup ON office_workspace_snapshots(state,updated_at);
