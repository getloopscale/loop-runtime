# Email Flows — Verification, Recovery, Settings

## Objective
Make email verification, password recovery, and account settings fully functional on DigitalOcean with Resend as SMTP provider.

---

## Phase 1 — SMTP via Resend [30min]

### Tasks
1. Add `LOOPZERO_SMTP_CONNECTION_URI` to GitHub Actions secrets (`infra-dev` environment)
   - Format: `smtps://resend:re_XXXX@smtp.resend.com:465`
2. Wire env var in `infra-deploy.yml` → Pulumi job
3. Update `stack.ts` to read `LOOPZERO_SMTP_CONNECTION_URI` from env (same pattern as OAuth)
4. Pulumi already passes `smtpConnectionUri` to Kratos config (line 543-554 of mgmt-bootstrap.ts)

### DoD
- [ ] Kratos pod restarts with real SMTP config (not localhost:1025)
- [ ] `kubectl logs deploy/ory-kratos` shows no SMTP connection errors
- [ ] Test: create account → verification email received at real email address

---

## Phase 2 — Kratos UI URLs [15min]

### Tasks
1. Update `installOryKratos` in `mgmt-bootstrap.ts` to override selfservice URLs:
   ```
   selfservice.default_browser_return_url: https://dashboard.{domain}/
   selfservice.flows.login.ui_url: https://dashboard.{domain}/login
   selfservice.flows.registration.ui_url: https://dashboard.{domain}/register
   selfservice.flows.verification.ui_url: https://dashboard.{domain}/verification
   selfservice.flows.recovery.ui_url: https://dashboard.{domain}/recovery
   selfservice.flows.settings.ui_url: https://dashboard.{domain}/settings
   selfservice.flows.error.ui_url: https://dashboard.{domain}/error
   selfservice.flows.logout.after.default_browser_return_url: https://dashboard.{domain}/login
   ```

### DoD
- [ ] Kratos redirects to `dashboard.dev.loopzero.example/*` (not localhost:4455)
- [ ] Verification email link points to `dashboard.dev.loopzero.example/verification?flow=...`
- [ ] Recovery email link points to `dashboard.dev.loopzero.example/recovery?flow=...`

---

## Phase 3 — Dashboard Pages [2-3h]

### 3.1 Registration Page (`/register`)
- Form: email + password + name
- Calls Kratos `/self-service/registration/browser` to initiate flow
- Renders Kratos form nodes (CSRF token, input fields, submit)
- On success: redirect to `/verification` with message "Check your email"

**DoD:**
- [ ] User can create account via email+password at `dashboard.dev.loopzero.example/register`
- [ ] Form shows validation errors from Kratos (weak password, duplicate email)
- [ ] Successful registration triggers verification email

### 3.2 Verification Page (`/verification`)
- Handles `?flow=<id>&code=<code>` from email link
- Calls Kratos `/self-service/verification/browser` to check/submit flow
- Shows: "Email verified!" on success, "Invalid or expired link" on failure
- Button to resend verification email

**DoD:**
- [ ] Clicking email link completes verification
- [ ] Shows success message after verification
- [ ] Expired link shows clear error with resend option

### 3.3 Recovery Page (`/recovery`)
- Form: email input
- Calls Kratos `/self-service/recovery/browser` to initiate flow
- After email sent: shows "Check your email for recovery link"
- When user clicks link: shows password reset form
- Calls Kratos to submit new password

**DoD:**
- [ ] User enters email → receives recovery email
- [ ] Clicking recovery link shows password reset form
- [ ] New password is saved, user can login with it

### 3.4 Settings Page (`/settings`)
- Change password form
- Calls Kratos `/self-service/settings/browser`
- Shows current profile (email, name)
- Connected social accounts (GitHub, Google)

**DoD:**
- [ ] User can change password from settings page
- [ ] Shows which social accounts are connected
- [ ] Password change requires current password

### 3.5 Error Page (`/error`)
- Handles `?id=<error-id>` from Kratos
- Calls Kratos `/self-service/errors?id=<id>` to get error details
- Displays user-friendly error message (no internal terms)

**DoD:**
- [ ] Kratos errors render as user-friendly messages
- [ ] No internal terms (Kratos, Ory, etc.) visible

---

## Phase 4 — E2E Validation [30min]

### Test Scenarios
1. **Registration + Verification**: Register → check email → verify → login
2. **Password Recovery**: Request recovery → check email → reset password → login with new password
3. **Social Login (GitHub)**: Click GitHub → authorize → logged in
4. **Social Login (Google)**: Click Google → authorize → logged in
5. **Settings**: Login → change password → logout → login with new password

### DoD
- [ ] All 5 scenarios pass on `dashboard.dev.loopzero.example`
- [ ] No internal terms in any user-facing page
- [ ] Emails delivered within 30 seconds via Resend

---

## Dependencies

| Phase | Depends On | Blocker? |
|-------|-----------|----------|
| Phase 1 | Resend API key from user | Yes |
| Phase 2 | Phase 1 (SMTP must work) | No (can deploy in parallel) |
| Phase 3 | Phase 2 (URLs must point to dashboard) | Yes |
| Phase 4 | Phase 1 + 2 + 3 | Yes |

## Estimated Effort: 3-4 hours total
