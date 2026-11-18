---
name: xgrowth
description: Run an X (Twitter) growth studio from the CLI — watch accounts and searches, triage content, draft posts, replies and quotes with Claude, and queue them for human validation and scheduled publishing. Use when an agent should prepare X content for a human to approve. Every command prints one JSON envelope.
---

# xgrowth — X growth studio for agents

Every command prints one JSON envelope on stdout. Check `.ok`, read `.data`; on error read `.error.message`.
Add `--pretty` only when a human reads the output.

## Hard rule

**Never run `xgrowth validate`.** Validation is the human's signature that allows publishing. Prepare drafts, then
tell the human which post ids are waiting and that they validate them in the studio (`xgrowth serve`) or with
`xgrowth validate <id> --by <their name>`. Never run `publish-due` or `run` unless the human asked for it.

## Read

```
account list                      accounts, voices, slots, watch targets
posts [--status draft|scheduled|published|failed|rejected] [--account a] [-n 50]
show <id>                         one post with its history
items [--status pending|kept|ignored|drafted] [--account a]
calendar [--days 14]              planned and published posts by day
learnings                         the owner's feedback, applied to every draft
log [-n 50]
```

## Watch and triage

```
watch add <account> --user handle | --search "query" [--every 60]
fetch [--account a]               poll every watch target now
bookmarks <account> [-n 20]       import X bookmarks
item add <account> <url>          add one X post to the watch list
triage [--account a] [--draft]    Claude scores pending items; --draft writes a draft for each kept item
item keep|ignore <id>
```

## Draft

```
draft <account> --topic "brief" [-n 3]     Claude drafts n posts
draft <account> --item <item id> [-n 2]    drafts from a watched post
reply <account> <url|id> [--quote]         Claude drafts a reply (or quote)
write <account> "text" [--reply-to id] [--quote-of id] [--media file]   your own draft
edit <id> "new text"                       any edit sends the post back to draft
rewrite <id> --feedback "shorter, one idea" Claude rewrites; the feedback becomes a lesson
reject <id> --reason "why"                 the reason becomes a lesson
learn "lesson" [--account a]
```

Drafts must pass the guard before a human can validate them: no competitor names (`settings`), no banned phrases,
280 characters max (4000 for Premium accounts), no exact repeat of a published post. Fix a failing draft with
`edit` or `rewrite` before handing it to the human.
