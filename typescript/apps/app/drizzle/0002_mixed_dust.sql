CREATE TABLE `memberships` (
	`org_did` text NOT NULL,
	`member_did` text NOT NULL,
	`roles` text NOT NULL,
	`updated_at` integer NOT NULL,
	PRIMARY KEY(`org_did`, `member_did`)
);
--> statement-breakpoint
CREATE INDEX `memberships_member` ON `memberships` (`member_did`);