CREATE TABLE `comments` (
	`uri` text PRIMARY KEY NOT NULL,
	`doc_space_uri` text NOT NULL,
	`thread_id` text NOT NULL,
	`author_did` text NOT NULL,
	`body` text NOT NULL,
	`quoted_text` text,
	`resolved` integer DEFAULT false NOT NULL,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE INDEX `comments_doc_created` ON `comments` (`doc_space_uri`,`created_at`);--> statement-breakpoint
CREATE INDEX `comments_thread` ON `comments` (`doc_space_uri`,`thread_id`);