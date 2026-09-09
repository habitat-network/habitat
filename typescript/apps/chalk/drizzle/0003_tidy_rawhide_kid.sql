CREATE TABLE `comment_replies` (
	`uri` text PRIMARY KEY NOT NULL,
	`doc_space_uri` text NOT NULL,
	`comment_uri` text NOT NULL,
	`author_did` text NOT NULL,
	`body` text NOT NULL,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE INDEX `comment_replies_thread` ON `comment_replies` (`doc_space_uri`,`comment_uri`);--> statement-breakpoint
CREATE TABLE `comments` (
	`uri` text PRIMARY KEY NOT NULL,
	`cid` text NOT NULL,
	`doc_space_uri` text NOT NULL,
	`author_did` text NOT NULL,
	`body` text NOT NULL,
	`anchor_start` text NOT NULL,
	`anchor_end` text NOT NULL,
	`quoted_text` text,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE INDEX `comments_doc_created` ON `comments` (`doc_space_uri`,`created_at`);