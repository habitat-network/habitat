DROP TABLE `sync_repos`;--> statement-breakpoint
DROP TABLE `sync_spaces`;--> statement-breakpoint
DROP TABLE `messages`;--> statement-breakpoint
DROP TABLE `chats`;--> statement-breakpoint
CREATE TABLE `chats` (
	`id` text PRIMARY KEY NOT NULL,
	`owner_did` text NOT NULL,
	`title` text NOT NULL,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE INDEX `chats_owner_created` ON `chats` (`owner_did`,`created_at`);--> statement-breakpoint
CREATE TABLE `messages` (
	`chat_id` text NOT NULL,
	`id` text NOT NULL,
	`role` text NOT NULL,
	`content` text NOT NULL,
	`created_at` integer NOT NULL,
	PRIMARY KEY(`chat_id`, `id`),
	FOREIGN KEY (`chat_id`) REFERENCES `chats`(`id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
CREATE INDEX `messages_chat_created` ON `messages` (`chat_id`,`created_at`);
