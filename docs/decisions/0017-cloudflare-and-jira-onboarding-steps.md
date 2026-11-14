# 0017 — Onboarding adds the Cloudflare address and the Jira customer

- **Status:** Accepted
- **Date:** 2026-11-14

## Context
During the first real onboarding of a new member, the plan still said "do this by hand" for Cloudflare
and Jira. The webmaster pointed out that doing these is the point of the tool. Both steps were
next in the Phase 2 build order. The tool already had what it needed to add a Cloudflare
destination address, because the verification re-send uses it ([0016](0016-resend-cloudflare-verification.md)).

## Decision
**Cloudflare (mailing list).** Onboarding adds the member as a **destination address**, which
makes Cloudflare email them a verification link.
- It checks again first: an address that's already there (verified or waiting) isn't added
  twice, so they never get a second verification email.
- **Adding them to the all-members list (`LIST_NEWS`) isn't automated yet.** On the test account,
  the plan says that part is still by hand. The APS account has no list Worker until the
  December switch-over, so there's nothing to add them to yet.

**Jira.** Onboarding makes them a **customer** on the APS service desk:
- `POST /rest/servicedeskapi/customer` creates the customer account. If an Atlassian account
  already exists for the email, it's looked up instead.
- Then `POST /rest/servicedeskapi/servicedesk/{id}/customer` adds it to the desk.
- It checks the desk first, so an existing customer isn't added again.
- A deactivated customer stays a by-hand problem.

**Atlassian emails new customers.** Atlassian's documentation says adding a customer to a service
desk sends them an email: an invitation to sign up, or a "verify your email" message, depending on
the desk's "Customer invited" notification. Members don't know Jira exists, so the webmaster
chose to **explain it in the welcome email**. A new `{{HELP_DESK_NAME}}` placeholder, filled from
the Jira settings, names the desk, and the example template says: "You may also get an email
from Atlassian about our help desk, the {{HELP_DESK_NAME}}. It just makes sure your requests for
help reach us. You don't need to do anything with it."

## Consequences
- A normal onboarding is now: check the plan, press Go (Cloudflare address, Jira customer, forum
  invite), then send the welcome email.
- The webmaster's Jira account must be allowed to add customers (an agent or admin on the desk).
  Cloudflare needs the token's **Email Routing Addresses: Edit** permission.
- Each change is checked first, recorded in the activity log, and only made after Go.
