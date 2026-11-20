CREATE TABLE `comment_replies` (
	`uri` text PRIMARY KEY NOT NULL,
	`doc_space_uri` text NOT NULL,
	`comment_uri` text NOT NULL,
	`author_did` text NOT NULL,
	`body` text NOT NULL,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE INDEX `comment_replies_thread` ON `comment_replies` (`doc_space_uri`,`comment_uri`);--> statement-breakpoint
CREATE TABLE `comments` (
	`uri` text PRIMARY KEY NOT NULL,
	`cid` text NOT NULL,
	`doc_space_uri` text NOT NULL,
	`author_did` text NOT NULL,
	`body` text NOT NULL,
	`anchor_start` blob NOT NULL,
	`anchor_end` blob NOT NULL,
	`quoted_text` text,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE INDEX `comments_doc_created` ON `comments` (`doc_space_uri`,`created_at`);--> statement-breakpoint
CREATE TABLE `connected_orgs` (
	`org_did` text PRIMARY KEY NOT NULL,
	`name` text NOT NULL,
	`connected_by` text NOT NULL,
	`connected_at` integer NOT NULL
);
--> statement-breakpoint
CREATE TABLE `doc_access` (
	`subject_did` text NOT NULL,
	`space_uri` text NOT NULL,
	`uri` text NOT NULL,
	`relation` text NOT NULL,
	`updated_at` integer NOT NULL,
	PRIMARY KEY(`subject_did`, `space_uri`)
);
--> statement-breakpoint
CREATE INDEX `doc_access_uri` ON `doc_access` (`uri`);--> statement-breakpoint
CREATE TABLE `doc_org_access` (
	`org_did` text NOT NULL,
	`space_uri` text NOT NULL,
	`uri` text NOT NULL,
	`relation` text NOT NULL,
	`updated_at` integer NOT NULL,
	PRIMARY KEY(`org_did`, `space_uri`)
);
--> statement-breakpoint
CREATE INDEX `doc_org_access_uri` ON `doc_org_access` (`uri`);--> statement-breakpoint
CREATE TABLE `docs` (
	`space_uri` text PRIMARY KEY NOT NULL,
	`doc_id` text NOT NULL,
	`owner_did` text NOT NULL,
	`title` text NOT NULL,
	`updated_at` integer NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `docs_doc_id_unique` ON `docs` (`doc_id`);--> statement-breakpoint
CREATE INDEX `docs_owner_updated` ON `docs` (`owner_did`,`updated_at`);--> statement-breakpoint
CREATE TABLE `files` (
	`uri` text PRIMARY KEY NOT NULL,
	`cid` text NOT NULL,
	`space` text NOT NULL,
	`repo` text NOT NULL,
	`org_did` text NOT NULL,
	`name` text NOT NULL,
	`blob_cid` text NOT NULL,
	`mime_type` text NOT NULL,
	`size` integer NOT NULL,
	`uploaded_by` text,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE INDEX `files_org_created` ON `files` (`org_did`,`created_at`);--> statement-breakpoint
CREATE INDEX `files_space_repo` ON `files` (`space`,`repo`);--> statement-breakpoint
CREATE TABLE `sync_repos` (
	`space` text NOT NULL,
	`did` text NOT NULL,
	`rev` text NOT NULL,
	`lt_hash` blob NOT NULL,
	PRIMARY KEY(`space`, `did`)
);
--> statement-breakpoint
CREATE TABLE `sync_spaces` (
	`space` text PRIMARY KEY NOT NULL,
	`authority` text NOT NULL,
	`space_rev` text,
	`registration_expires_at` integer,
	`next_due_at` integer NOT NULL,
	`last_full_pass_at` integer,
	`failures` integer NOT NULL,
	`last_error` text
);
--> statement-breakpoint
CREATE INDEX `sync_spaces_due` ON `sync_spaces` (`next_due_at`);