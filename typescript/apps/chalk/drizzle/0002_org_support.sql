ALTER TABLE `docs` ADD `is_org` integer DEFAULT false NOT NULL;
--> statement-breakpoint
CREATE TABLE `connected_orgs` (
	`member_did` text NOT NULL,
	`org_did` text NOT NULL,
	`org_name` text NOT NULL,
	`connected_at` integer NOT NULL,
	PRIMARY KEY(`member_did`, `org_did`)
);
