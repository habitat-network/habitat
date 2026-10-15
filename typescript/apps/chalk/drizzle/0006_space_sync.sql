CREATE TABLE `oauth_sessions` (
	`did` text PRIMARY KEY NOT NULL,
	`value` blob NOT NULL,
	`updated_at` integer NOT NULL
);
--> statement-breakpoint
CREATE TABLE `oauth_states` (
	`key` text PRIMARY KEY NOT NULL,
	`value` blob NOT NULL,
	`expires_at` integer NOT NULL
);
--> statement-breakpoint
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