DROP INDEX `chats_owner_created`;--> statement-breakpoint
ALTER TABLE `chats` ADD `org_did` text DEFAULT '' NOT NULL;--> statement-breakpoint
CREATE INDEX `chats_owner_org_created` ON `chats` (`owner_did`,`org_did`,`created_at`);