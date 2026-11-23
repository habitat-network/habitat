CREATE TABLE `apps` (
	`uri` text PRIMARY KEY NOT NULL,
	`cid` text NOT NULL,
	`space` text NOT NULL,
	`repo` text NOT NULL,
	`org_did` text NOT NULL,
	`host` text NOT NULL,
	`name` text NOT NULL,
	`slug` text NOT NULL,
	`description` text,
	`entrypoint` text NOT NULL,
	`code_cid` text NOT NULL,
	`code_size` integer NOT NULL,
	`version` integer NOT NULL,
	`compatibility_date` text NOT NULL,
	`created_by` text,
	`updated_by` text,
	`created_at` integer NOT NULL,
	`updated_at` integer NOT NULL
);
--> statement-breakpoint
CREATE INDEX `apps_org_updated` ON `apps` (`org_did`,`updated_at`);--> statement-breakpoint
CREATE INDEX `apps_host` ON `apps` (`host`);--> statement-breakpoint
CREATE INDEX `apps_space_repo` ON `apps` (`space`,`repo`);