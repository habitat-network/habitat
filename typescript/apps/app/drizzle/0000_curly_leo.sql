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
