CREATE TABLE `doc_org_access` (
	`org_did` text NOT NULL,
	`space_uri` text NOT NULL,
	`uri` text NOT NULL,
	`relation` text NOT NULL,
	`updated_at` integer NOT NULL,
	PRIMARY KEY(`org_did`, `space_uri`)
);
--> statement-breakpoint
CREATE INDEX `doc_org_access_uri` ON `doc_org_access` (`uri`);