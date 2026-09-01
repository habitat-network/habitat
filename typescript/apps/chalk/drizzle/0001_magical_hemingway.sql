CREATE TABLE `doc_access` (
	`subject_did` text NOT NULL,
	`space_uri` text NOT NULL,
	`uri` text NOT NULL,
	`relation` text NOT NULL,
	`updated_at` integer NOT NULL,
	PRIMARY KEY(`subject_did`, `space_uri`)
);
--> statement-breakpoint
CREATE INDEX `doc_access_uri` ON `doc_access` (`uri`);