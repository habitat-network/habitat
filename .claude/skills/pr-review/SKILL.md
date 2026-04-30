---
name: pr-review
description: Use this skill when reviewing PR comments and addressing them on a branch.
---

# PR Review workflow

## Before starting
1. Read `.claude/skills/pr-review/learnings.md` and summarize the key entries
2. Then proceed with the review

## Steps
1. Read all the review comments on the PR. 
2. Address any non-trivial (syntax, preferred component or library, stylistic, naming, straightforward) comments. Do not address feedback on API interfaces or asks for major refactors. For these large changes, tag @arushibandi on Github to address the feedback.

## After completing
1. Update `.claude/skills/pr-review/learnings.md` with any new observations that are applicable to the entire repo or generalized tasks in a succinct manner. Do not over-write into this file.