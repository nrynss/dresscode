//go:build live

package memory

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// liveSession is one scratch id for this run. The probe ids are tombstoned.
const liveSession = "dc-u-live-t23b"

// TestLiveRememberRecallErase remembers one look, recalls it, and erases
// the session on the Dresscode Lambo server. The gate does not build this
// file.
func TestLiveRememberRecallErase(t *testing.T) {
	if liveSession == "dc-u-probe-a" || liveSession == "dc-u-probe-b" || !ValidSession(liveSession) {
		t.Fatal("live session id is not a fresh dc-u- id")
	}
	base, token, err := loadLiveLambo()
	if err != nil {
		t.Fatal(err)
	}
	if err := guardLiveURL(base, token); err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{BaseURL: base, Token: token})
	if err != nil {
		liveFatal(t, token, err)
	}

	// A failed check still erases, so the scratch text does not stay.
	// The id does not come back after that.
	erased := false
	t.Cleanup(func() {
		if erased {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := client.Erase(ctx, liveSession)
		if err != nil {
			t.Errorf("cleanup erase: %s", scrubToken(token, err.Error()))
			return
		}
		liveLog(t, token, "cleanup erase session=%s already_absent=%v concepts=%d",
			result.Session, result.AlreadyAbsent, result.Removed.Concepts)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	look := []Concept{
		{Content: "Live check kept the navy wrap.", ConceptType: ConceptObservation},
		{Content: "Live check dismissed the extra belt.", ConceptType: ConceptObservation},
	}
	remembered, err := client.Remember(ctx, liveSession, look)
	if err != nil {
		liveFatal(t, token, "remember: "+err.Error())
	}
	liveLog(t, token, "remember session=%s state=%s receipt=%s created=%d embedded=%d agent=%s summary=%s warnings=%d",
		liveSession, remembered.State, remembered.Receipt, remembered.CreatedCount, remembered.Embedded,
		remembered.AgentID, remembered.Summary, len(remembered.Warnings))
	if remembered.State != "applied" || remembered.CreatedCount != 2 || remembered.Embedded != 2 || remembered.AgentID != DefaultAgentID {
		liveFatal(t, token, fmt.Sprintf("remember not applied: state=%s created=%d embedded=%d agent=%s",
			remembered.State, remembered.CreatedCount, remembered.Embedded, remembered.AgentID))
	}

	const occasion = "live check"
	recalled, err := client.Recall(ctx, liveSession, occasion)
	if err != nil {
		liveFatal(t, token, "recall: "+err.Error())
	}
	liveLog(t, token, "recall session=%s query=%q hits=%d warnings=%d",
		liveSession, occasion, len(recalled.Hits), len(recalled.Warnings))
	for _, warning := range recalled.Warnings {
		liveLog(t, token, "recall warning %s", warning)
	}
	var text string
	for _, hit := range recalled.Hits {
		liveLog(t, token, "recall hit type=%s content=%q", hit.ConceptType, hit.Content)
		text += hit.Content + "\n"
	}
	if len(recalled.Hits) != 2 || !strings.Contains(text, "navy wrap") || !strings.Contains(text, "extra belt") {
		liveFatal(t, token, "recall did not return the look")
	}
	for _, banned := range []string{"gold jhumkas", "red lip", "silver studs", "braid"} {
		if strings.Contains(text, banned) {
			liveFatal(t, token, "recall returned another session's look")
		}
	}

	result, err := client.Erase(ctx, liveSession)
	if err != nil {
		liveFatal(t, token, "erase: "+err.Error())
	}
	erased = true
	liveLog(t, token, "erase session=%s already_absent=%v concepts=%d vectors=%d sessions=%d fence=%d",
		result.Session, result.AlreadyAbsent, result.Removed.Concepts, result.Removed.Vectors,
		result.Removed.Sessions, result.FenceToken)
	if result.AlreadyAbsent || result.Session != liveSession || result.Removed.Concepts != 2 || result.Removed.Vectors != 2 {
		liveFatal(t, token, fmt.Sprintf("erase did not remove the look: absent=%v session=%s concepts=%d vectors=%d",
			result.AlreadyAbsent, result.Session, result.Removed.Concepts, result.Removed.Vectors))
	}
}

// loadLiveLambo reads LAMBO_URL and LAMBO_DRESSCODE_TOKEN from the
// environment. A missing name fails with that name and no value.
func loadLiveLambo() (string, string, error) {
	const urlName = "LAMBO_URL"
	const tokenName = "LAMBO_DRESSCODE_TOKEN"
	base := os.Getenv(urlName)
	token := os.Getenv(tokenName)
	switch {
	case base == "" && token == "":
		return "", "", fmt.Errorf("memory: %s and %s are missing", urlName, tokenName)
	case base == "":
		return "", "", fmt.Errorf("memory: %s is missing", urlName)
	case token == "":
		return "", "", fmt.Errorf("memory: %s is missing", tokenName)
	}
	return base, token, nil
}

// guardLiveURL refuses the other Lambo serves. This test dials only the
// Dresscode server.
func guardLiveURL(base, token string) error {
	if strings.Contains(base, token) {
		return errors.New("memory: lambo url must not carry the token")
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.Port() == "" {
		return errors.New("memory: lambo url is not a host and port")
	}
	switch u.Port() {
	case "7700", "7710":
		return errors.New("memory: refusing lambo ports 7700 and 7710")
	case "7720":
	default:
		return errors.New("memory: live lambo url is not port 7720")
	}
	if u.Hostname() != "127.0.0.1" || u.Scheme != "http" {
		return errors.New("memory: live lambo url is not http://127.0.0.1:7720")
	}
	return nil
}

func liveLog(t *testing.T, token, format string, args ...any) {
	t.Helper()
	t.Log(scrubToken(token, fmt.Sprintf(format, args...)))
}

func liveFatal(t *testing.T, token string, args ...any) {
	t.Helper()
	t.Fatal(scrubToken(token, fmt.Sprint(args...)))
}

func scrubToken(token, text string) string {
	if token == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "redacted")
}
