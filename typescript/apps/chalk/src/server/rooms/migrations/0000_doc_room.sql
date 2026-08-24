CREATE TABLE `doc_state` (
	`id` integer PRIMARY KEY NOT NULL,
	`space_uri` text NOT NULL,
	`owner_did` text,
	`state` blob,
	`updated_at` integer NOT NULL
);
--> statement-breakpoint
CREATE TABLE `pending_flush` (
	`kind` text NOT NULL,
	`member_did` text NOT NULL,
	`first_push_at` integer NOT NULL,
	`idle_deadline` integer NOT NULL,
	PRIMARY KEY(`kind`, `member_did`)
);
