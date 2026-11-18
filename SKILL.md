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
Reordering planned posts (`reorder <id> <id>…`, `move <id> <YYYY-MM-DD>`) changes when they go out: only on request.

## Read

```
account list                      accounts, voices, slots, watch targets
posts [--status draft|scheduled|published|failed|rejected] [--account a] [-n 50]
show <id>                         one post with its history
items [--status pending|kept|ignored|drafted] [--account a]
calendar [--days 14]              planned and published posts by day
learnings                         the owner's feedback, applied to every draft
rules [list]                      common writing rules and learned rules (proposed, accepted, refused, revoked)
log [-n 50]
```

## Sources

```
sources [--account a]                      fetch the product changelog and GitHub stars now (new entries go to the watch list)
sources news                               draft the news of primary sources whose thread has settled
sources status
watch primary <id> on|off                  mark a followed account as a primary source (only when the human asks)
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

## Reply campaigns (prospecting)

```
campaign list                              campaigns, replies sent today, replies waiting
campaign run <id>                          search, qualify and draft replies now (uses the search budget)
campaign candidates [<id>] [--status new|drafted|dismissed]
comments [--campaign id] [--status draft]  replies drafted by campaigns
```

Campaign replies are drafts like any other: never validate them. Create or edit a campaign (`campaign save`) only
when the human asks; an active pain needs a verified solution.

## Draft

```
draft <account> --topic "brief" [-n 3] [--type key]   Claude drafts n posts (of one post type if given)
draft <account> --item <item id> [-n 2]    drafts from a watched post
reply <account> <url|id> [--quote]         Claude drafts a reply (or quote)
write <account> "text" [--reply-to id] [--quote-of id] [--media file]   your own draft
edit <id> "new text"                       any edit sends the post back to draft
rewrite <id> --feedback "shorter, one idea" Claude rewrites; the feedback becomes a lesson
reject <id> --reason "why"                 the reason becomes a lesson
learn "lesson" [--account a]
rules synthesize [--account a]             the AI turns unsynthesized feedback into proposed rules
ideas [--account a] [--status proposed|kept|dismissed|drafted]
ideas suggest <account>                    the AI proposes original post ideas (idea bank must be on)
ideas draft <id>                           drafts the post of an idea (it waits for validation)
```

## Tune the AI (only when the human asks)

The owner's editorial settings guide every draft: goals, length, reference posts, post types, per-request
instructions and rules. Read them with `account show <account>` and `settings`; change them only on request:

```
account set <account> [--goals "..."] [--length court|moyen|long] [--examples-file posts.txt]
                      [--post-types JSON|@file.json|defaut] [--ideas-prompt|--item-prompt|--reply-prompt
                      |--quote-prompt|--rewrite-prompt|--visual-guide "..."]   (empty = inherit)
settings [--rules-file rules.txt] [--post-types ...] [--<kind>-prompt "..."] [--visual-guide "..."]
```

Drafts may contain `[À COMPLÉTER : …]` markers where real experience or figures are needed: never fill them with
invented content. Validation refuses them; tell the human which drafts need their input. Keeping or dismissing an
idea (`ideas keep|dismiss`) and categorizing feedback (`learnings set`) are the human's choices too.

Never accept, refuse or revoke a learned rule yourself (`rules accept|refuse|revoke`): like validation, it is the
human's decision. Tell them which rules wait in Settings.

Drafts must pass the guard before a human can validate them: no competitor names (`settings`), no banned phrases,
280 characters max (4000 for Premium accounts), no exact repeat of a published post. Fix a failing draft with
`edit` or `rewrite` before handing it to the human.
