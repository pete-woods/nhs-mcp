# nhs-mcp

An unofficial MCP server for the NHS App. It lets an agent read your own GP
record, test results above all, through the API behind
[www.nhsapp.service.nhs.uk](https://www.nhsapp.service.nhs.uk).

> [!WARNING]
> This isn't supported by the NHS. It uses the NHS App's private web API, which
> can change without notice. Everything the tools return goes to your agent's
> model provider, so only use it with your own record, and an agent you're
> happy to share it with.

## Install

Requires Go and Google Chrome (or Chromium/Edge; without one, Chromium is
downloaded on first login).

```sh
task build                                  # builds bin/nhs-mcp
claude mcp add nhs-app -- "$PWD/bin/nhs-mcp" # add it to Claude Code
```

Any MCP client that runs stdio servers works the same way: run `bin/nhs-mcp`.

## Check it works

1. Start a new Claude Code session, so it picks up the server, and check
   `/mcp` lists `nhs-app` as connected.
2. Ask: *"Log in to the NHS App."* A Chrome window opens at NHS login. Sign
   in with your email, password and security code. The window closes itself
   when you're done, and the agent gets `logged_in: true, gp_session: true`.
3. Ask: *"What are my most recent test results?"* (`list_test_results`), then
   *"Show me the first one"* (`get_test_result`).
4. Ask: *"How has my &lt;measurement&gt; changed over time?"*, using a name from
   `list_test_names` (`get_test_trend`).
5. Ask: *"What letters are in my record?"* (`list_documents`), then *"Read
   me the latest one"* (`get_document`).
6. Ask: *"Log me out of the NHS App."*

If something goes wrong, set `NHS_MCP_DEBUG=1` (`claude mcp add -e
NHS_MCP_DEBUG=1 …`). Each API call is then logged to stderr, by path and
status only: no query strings, tokens or record contents.

## How login works

The NHS App's API uses a cookie session, so there's no token to paste. The
`login` tool:

1. Opens NHS login in Chrome as the NHS App does (OIDC with PKCE). You sign
   in yourself, and the browser handles NHS login's bot protection.
2. Catches the redirect back to the NHS App before it loads, and exchanges
   the code for an NHS App session itself.
3. Starts a GP session the same way the app does: it gets an asserted login
   identity from the NHS App, then signs in to NHS login again silently in
   the same window.

The browser profile is kept (in `~/Library/Application Support/nhs-mcp/browser`
on macOS), so NHS login can remember the device and skip the security code.
Some GP systems send test results a year at a time instead of all at once;
for those, the tools fetch the last ten years unless given an earlier `since`,
and say so. Others don't break results into values; for those,
`get_test_result` fetches the result's text instead.

Sessions end after about ten minutes idle, after which the tools tell the
agent to call `login` again. The record is fetched once and cached for 15
minutes.

## Tools

| Tool | What it does |
|---|---|
| `login`, `get_login_status`, `logout` | Sign in, check, sign out |
| `list_test_results` | Results, newest first; filter by date, name, or out-of-range |
| `get_test_result` | Every value in a result, with range, low/high flag and notes |
| `get_test_trend` | One measurement over time, e.g. "Serum ferritin" |
| `list_test_names` | Test and measurement names to search with |
| `list_consultations` | Appointments: who you saw, and what they recorded under each heading |
| `list_documents` | Letters, scans and attachments |
| `get_document` | A document as text, or for scans and PDFs as page images the agent can read, with a link to the original file |
| `get_medications`, `get_allergies`, `get_problems`, `get_immunisations` | The rest of the GP record |
| `get_demographics` | Name, date of birth, address, NHS number |

The original files are MCP resources, `nhs-app://documents/{id}/file`, which `get_document` links to.

## Configuration

All environment variables, all optional:

| Variable | Default | |
|---|---|---|
| `NHS_MCP_LOGIN` | `browser` | `http` follows NHS login's redirects without a browser; it only works when no sign-in is needed |
| `NHS_MCP_BROWSER` | an installed Chrome | Path to the Chrome binary |
| `NHS_MCP_PROFILE_DIR` | `<user config dir>/nhs-mcp/browser` | Browser profile |
| `NHS_MCP_DEBUG` | off | Debug logging to stderr |
| `NHS_MCP_AUTH_URL`, `NHS_MCP_API_URL`, `NHS_MCP_WEB_URL` | the real NHS | For tests |

## Development

See [AGENTS.md](AGENTS.md). `task test` runs everything against a fake NHS
login and NHS App (`internal/testing/fakenhs`), with a synthetic record; the
acceptance tests run the built binary over stdio, with any request that
tries to reach the real NHS caught and failed. `task check` lints.

Never commit captured traffic (`*.har`): it holds live tokens and health
records.
