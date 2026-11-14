package cli

import (
	"testing"

	"github.com/yippieskippie24/aps-tools/internal/connectors"
	"github.com/yippieskippie24/aps-tools/internal/followup"
	"github.com/yippieskippie24/aps-tools/internal/roster"
)

func TestFollowupsChoosesWho(t *testing.T) {
	if k, ok := kindByID("Setup"); !ok || k != followup.NeedsSetup {
		t.Errorf("setup → %v %v", k, ok)
	}
	if _, ok := kindByID("everyone"); ok {
		t.Error("unknown follow-ups should be refused")
	}
	l := followup.List{Kind: followup.Unverified, Members: []roster.Entry{
		{Person: connectors.Person{Email: "amy@example.com"}},
		{Person: connectors.Person{Email: "bo@example.com"}},
	}}
	got := chosenPeople(l, []string{" BO@example.com "})
	if len(got) != 1 || got[0].Email != "amy@example.com" {
		t.Errorf("--skip: %v", got)
	}
}
