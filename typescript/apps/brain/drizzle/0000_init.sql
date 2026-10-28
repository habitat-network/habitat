CREATE TABLE `chats` (
	`space` text PRIMARY KEY NOT NULL,
	`cid` text NOT NULL,
	`owner_did` text NOT NULL,
	`title` text NOT NULL,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE INDEX `chats_owner_created` ON `chats` (`owner_did`,`created_at`);--> statement-breakpoint
CREATE TABLE `messages` (
	`uri` text PRIMARY KEY NOT NULL,
	`cid` text NOT NULL,
	`space` text NOT NULL,
	`repo` text NOT NULL,
	`role` text NOT NULL,
	`content` text NOT NULL,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE INDEX `messages_space_created` ON `messages` (`space`,`created_at`);--> statement-breakpoint
CREATE INDEX `messages_space_repo` ON `messages` (`space`,`repo`);--> statement-breakpoint
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