//go:build live

package memory

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
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

// loadLiveLambo reads the Dresscode serve from the main checkout. A
// worktree has no .env. Other names in that file are not kept.
func loadLiveLambo() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	f, err := os.Open(filepath.Join(home, "work", "dresscode", ".env"))
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	var base, token string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if value[0] == value[len(value)-1] && (value[0] == '"' || value[0] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		switch key {
		case "LAMBO_URL":
			base = value
		case "LAMBO_DRESSCODE_TOKEN":
			token = value
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", err
	}
	if base == "" || token == "" {
		return "", "", errors.New("memory: live lambo url or token is missing")
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
