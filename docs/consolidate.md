# Consolidation

`amber consolidate` is the opt-in background maintenance pass (decision
D16). It merges duplicates, resolves contradictions via supersedence,
absolutizes relative dates ("last Tuesday" → a date), demotes aged
memories, and re-indexes.

**It never deletes.** Every action is a status transition or a content
rewrite, journaled to `ops` with enough payload to reverse it. Contrast
the field: some platforms prune aggressively enough that their own guides
tell users to back up first. Amber consolidates and keeps everything.

```sh
amber consolidate --dry-run     # plan; change nothing
amber consolidate               # apply
amber consolidate --since 30d   # only recently-updated memories
```

Output ends with the calm ledger:

```
consolidated: merged 3, resolved 1, dated 2, demoted 5, re-embedded 0 — deleted 0.
```

## Running it on a schedule

Consolidation is opt-in and runs from a scheduler **you** control.

### cron (Linux)

```cron
# 3am daily
0 3 * * * /usr/local/bin/amber consolidate --yes >> ~/.amber/logs/consolidate.log 2>&1
```

### launchd (macOS)

`~/Library/LaunchAgents/com.amber.consolidate.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.amber.consolidate</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/amber</string>
    <string>consolidate</string>
    <string>--yes</string>
  </array>
  <key>StartCalendarInterval</key>
  <dict><key>Hour</key><integer>3</integer><key>Minute</key><integer>0</integer></dict>
</dict>
</plist>
```

```sh
launchctl load ~/Library/LaunchAgents/com.amber.consolidate.plist
```

## Undoing a consolidation

Everything is reversible. `amber restore <id>` un-demotes an aged memory
or un-supersedes a merged one; `amber show <id>` shows the journal entry
with the prior state. Because the `ops` journal is append-only, nothing
is lost.
