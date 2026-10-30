package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestRecorderKeepsTheCodeOffTheLog(t *testing.T) {
	t.Parallel()
	rec := &Recorder{}
	if err := rec.Send(context.Background(), "Dresscode@nryn.dev", " "+signInCode+" "); err != nil {
		t.Fatal("send failed")
	}
	got := rec.Messages()
	if len(got) != 1 || got[0].Address != fromAddress || got[0].Code != signInCode {
		t.Fatal("recorded message does not match the send")
	}
	printed := fmt.Sprintf("%v %#v %s %+v %q", got[0], got[0], rec, rec, got[0])
	assertQuiet(t, printed, signInCode)
	var buf bytes.Buffer
	textLogger(&buf).Info("mail", "message", got[0], "recorder", rec)
	assertQuiet(t, buf.String(), signInCode)

	rec.SetFailure(errors.New("mail: down"))
	if err := rec.Send(context.Background(), fromAddress, signInCode); err == nil || len(rec.Messages()) != 1 {
		t.Fatal("failure was recorded or ignored")
	}
	rec.SetFailure(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rec.Send(ctx, fromAddress, signInCode); !errors.Is(err, context.Canceled) || len(rec.Messages()) != 1 {
		t.Fatal("cancelled send was recorded")
	}
	if err := rec.Send(context.Background(), fromAddress, "nope"); err == nil || len(rec.Messages()) != 1 {
		t.Fatal("a non-code was recorded")
	}
	var none *Recorder
	if err := none.Send(context.Background(), fromAddress, signInCode); err == nil {
		t.Fatal("nil recorder sent")
	}
}

func TestRecorderConcurrentSends(t *testing.T) {
	t.Parallel()
	rec := &Recorder{}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := rec.Send(context.Background(), fromAddress, signInCode); err != nil {
				t.Error("send failed")
			}
		}()
	}
	wg.Wait()
	if len(rec.Messages()) != 20 {
		t.Fatalf("%d messages, want 20", len(rec.Messages()))
	}
}
