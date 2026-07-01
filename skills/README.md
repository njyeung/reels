# Skills

These are [Claude Code](https://claude.com/claude-code) skills for the tedious, repetitive, maintenance tasks that come up in this project - mainly updating hardcoded Instagram GraphQL identifiers (`doc_id`, `friendly names`, `app id`) when Instagram changes their frontend API.

Each skill is a folder with a `SKILL.md`. To use one, copy the folder into your Claude Code skills directory:

```bash
# for example
cp -r skills/update-comments ~/.claude/skills/
```

Then invoke it from Claude Code. Claude will walk you through exactly where to navigate and the network-tab request data needed for the change.

You don't need Claude Code to contribute. These just automate what would otherwise be a manual find-and-replace. The `SKILL.md` files also double as human-readable documentation of which constants live where.

## Available skills

- **update-comments**: update the comments load + pagination GraphQL constants in `backend/graphql.go`.
