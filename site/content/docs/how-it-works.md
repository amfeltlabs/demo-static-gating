---
title: How it works
description: What happens between a browser request and a page leaving the binary.
weight: 10
---

Hugo builds this site into `server/dist`. The Go server embeds that folder at compile time, so the pages and the code that guards them ship as one file.

For every request the server does the following, in order:

1. **Passthrough.** If the path is on the passthrough list (the login pages and the assets they load), the gate steps aside.
2. **Session.** It reads the `hanko` cookie and asks the Hanko API whether the session is valid. No cookie, or an invalid session, earns a redirect to `/login/`, and the requested path is saved in a short-lived cookie.
3. **Role.** It looks up the session's user ID in the grants table. No required role earns a `403`, not a redirect: sending a signed-in user back to the login page would sign them straight back in and loop.
4. **File.** It serves the file from the embedded folder, with an `ETag` and a `Cache-Control` header that depends on whether the file name carries a content hash.

After signing in, Hanko sends the browser to `/post-login/`, which reads the saved path and sends the reader back where they started.
