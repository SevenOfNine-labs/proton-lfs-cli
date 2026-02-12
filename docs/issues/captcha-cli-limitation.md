# CAPTCHA Verification Has No CLI-Compatible Flow

## Problem

Proton's human verification page (`verify.proton.me`) uses `window.parent.postMessage()` to return the CAPTCHA token to the calling application. This mechanism is designed exclusively for web apps that embed the verification page in an iframe.

**CLI and API clients cannot receive the token**, because:

1. **No redirect callback** — There is no `redirect_uri` parameter. After CAPTCHA completion, the page posts the token via `postMessage` and nothing else happens. OAuth-style flows (used by `gh auth login`, `terraform login`, Stripe CLI, etc.) rely on the auth server redirecting back to `http://localhost:<port>/callback?token=...`. Proton's CAPTCHA page has no equivalent.

2. **No polling endpoint** — There is no API to check whether a CAPTCHA challenge has been completed. OAuth Device Flow (RFC 8628, used by GitHub CLI) works by having the CLI poll an endpoint. Proton's CAPTCHA has no such endpoint.

3. **No iframe embedding from localhost** — `verify.proton.me` sets CSP `frame-ancestors` that block embedding from non-Proton origins. A CLI starting a local HTTP server cannot embed the CAPTCHA in an iframe.

4. **IP allowlisting does not work** — Completing the CAPTCHA at `verify.proton.me` in a browser does not allowlist the IP for subsequent API requests. The CLI still receives a CAPTCHA challenge on retry.

## Impact

Every CLI or API client integrating with Proton authentication is affected:

- **[proton-drive-cli](https://github.com/miguelammatos/proton-drive-cli)** — README states: *"if a CAPTCHA is required it must be solved in the browser and the token manually copied back"* (listed under "What Could Work Better")
- **[proton-python-client](https://github.com/ProtonMail/proton-python-client)** — Same limitation applies to Python API clients
- **Any Git LFS adapter, backup tool, or automation** using Proton APIs

The manual token extraction requires users to open browser DevTools, find the `postMessage` payload or network request, and paste it into the CLI. This is not viable for non-technical users.

## Current Workaround

We reverse-proxy the CAPTCHA page through a local HTTP server:

1. CLI starts `http://localhost:<port>/captcha`
2. Server fetches `verify.proton.me` HTML, injects a `postMessage` listener script, strips CSP
3. Browser opens the proxied page (user sees Proton's real CAPTCHA widget)
4. When solved, injected script sends token to `http://localhost:<port>/callback`
5. CLI receives the token and retries authentication

**Limitations of this workaround:**

- Browser address bar shows `localhost` (looks untrustworthy)
- hCaptcha may reject solutions from `localhost` origin (site key domain mismatch)
- Fragile — depends on Proton's CAPTCHA page HTML structure not changing

## Proposed Solutions (for Proton)

Any of the following would enable clean CLI authentication:

### Option A: Redirect callback (like OAuth)

Add a `redirect_uri` parameter to the CAPTCHA page:

```
https://verify.proton.me/?methods=captcha&token=CHALLENGE&redirect_uri=http://localhost:PORT/callback
```

After CAPTCHA completion, redirect the browser to:

```
http://localhost:PORT/callback?verification_token=FULL_TOKEN
```

This is how GitHub, Anthropic, Stripe, Terraform, and virtually every other CLI auth flow works.

### Option B: Polling endpoint (like OAuth Device Flow)

Add an API endpoint to check CAPTCHA completion status:

```
GET /core/v4/captcha/status?token=CHALLENGE_TOKEN

→ { "completed": false }
→ { "completed": true, "verificationToken": "..." }
```

The CLI would:

1. Show: *"Open `https://verify.proton.me/...` and complete the CAPTCHA"*
2. Poll the status endpoint every 3 seconds
3. Receive the token when the user completes the CAPTCHA

No localhost server needed. No browser automation. Clean, trustworthy UX.

### Option C: Email/SMS verification for CLI clients

If `HumanVerificationMethods` includes `email` or `sms`, allow the CLI to request a verification code sent to the user's registered email/phone. The user enters the code in the terminal — no browser needed at all.

### Option D: Relax `frame-ancestors` CSP for localhost

Allow `verify.proton.me` to be embedded in iframes from `localhost` / `127.0.0.1`. This would let CLI tools use the iframe + `postMessage` pattern that web apps use, without requiring a reverse proxy.

## References

- [RFC 8628 — OAuth 2.0 Device Authorization Grant](https://datatracker.ietf.org/doc/html/rfc8628)
- [GitHub CLI auth flow](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow)
- [proton-drive-cli upstream issue](https://github.com/miguelammatos/proton-drive-cli) — *"PRs to fix the above are welcome"*
- Proton API error code `9001` — `HumanVerificationToken` + `HumanVerificationMethods`
- Proton API error code `2028` — May also contain `HumanVerificationToken` in Details

## Environment

- Proton Drive API: `drive-api.proton.me`
- Verification page: `verify.proton.me`
- Tested with: Firefox (macOS), CSP blocks iframe embedding
- Client: proton-drive-cli (TypeScript, Node.js 25)
