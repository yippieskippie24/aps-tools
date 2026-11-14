package cloudflare

import (
	"context"
	"strings"
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/connectors"
)

func TestOnboardAddsADestination(t *testing.T) {
	c, changes := writableCloudflare(t, resendRoutes(`[]`, `[]`), false)
	missing := connectors.MemberStatus{State: connectors.StateMissing}
	p := connectors.Person{Email: "new@example.com"}

	plan, err := c.PlanOnboard(context.Background(), p, missing)
	if err != nil || !strings.Contains(plan, "destination address") {
		t.Fatalf("plan = %q, %v", plan, err)
	}
	if len(*changes) != 0 {
		t.Fatal("planning must not change anything")
	}
	did, err := c.Onboard(context.Background(), p)
	if err != nil || !strings.Contains(did, "verification link") {
		t.Fatalf("did = %q, %v", did, err)
	}
	if strings.Join(*changes, "|") != "POST /accounts/acct/email/routing/addresses new@example.com" {
		t.Errorf("changes = %v", *changes)
	}
}

func TestOnboardSkipsSomeoneAlreadyAdded(t *testing.T) {
	c, changes := writableCloudflare(t, resendRoutes(pendingAddr, `[]`), false)
	p := connectors.Person{Email: "pat@example.com"}
	if plan, _ := c.PlanOnboard(context.Background(), p, connectors.MemberStatus{State: connectors.StateMissing}); plan != "" {
		t.Errorf("planned %q for someone already added", plan)
	}
	did, err := c.Onboard(context.Background(), p)
	if err != nil || !strings.Contains(did, "nothing to do") || len(*changes) != 0 {
		t.Errorf("did %q, %v, changes %v: must not add them twice (that would send another verification email)", did, err, *changes)
	}
}
