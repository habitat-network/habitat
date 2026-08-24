CREATE TABLE `docs` (
	`space_uri` text PRIMARY KEY NOT NULL,
	`doc_id` text NOT NULL,
	`owner_did` text NOT NULL,
	`title` text NOT NULL,
	`updated_at` integer NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `docs_doc_id_unique` ON `docs` (`doc_id`);--> statement-breakpoint
CREATE INDEX `docs_owner_updated` ON `docs` (`owner_did`,`updated_at`);
