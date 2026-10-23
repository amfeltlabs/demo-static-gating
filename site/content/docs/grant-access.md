---
title: Grant access
description: Give one person access, or take it away, without touching anyone else.
weight: 20
---

Access is a list of Hanko user IDs in `server/demo.toml`:

```toml
[[grant]]
  subject = "00000000-0000-0000-0000-000000000000"
  roles   = ["staff"]
```

Find a user's ID in the Hanko console, add a `[[grant]]` entry, and restart the server. Remove the entry and restart to revoke access. A signed-in user without the role sees a plain "Not authorized" page.

The subject is the key, never the email address. An email address can change hands; the ID cannot.
