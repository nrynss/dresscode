package privacy_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/dresscode/internal/pipeline"
	"github.com/nrynss/dresscode/internal/privacy"
	"github.com/nrynss/dresscode/internal/store"
	"github.com/nrynss/keel/erase"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/identity"
	identitysql "github.com/nrynss/keel/identity/sqlitestore"
	"github.com/nrynss/keel/job"
	jobsql "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/mediastore"
	mediasql "github.com/nrynss/keel/mediastore/sqlitestore"
	"github.com/nrynss/keel/sqlite"
)

// The tests here are T4.9's two store rules, and they share one rig shape: the
// landed store schema, Keel's job table, Keel's media index and a real
// mediastore over a temp directory, so a media id in this file is a minted
// 32-hex id whose bytes are on disk and whose row is in the same SQLite file
// the looks routes read. Nothing here dials: no key, no network, no model, and
// the pictures are one-pixel PNGs, not people.
//
// The sharing the reference count has to survive is constructed at store level,
// the way internal/server/looks.go's materialise writes it - two renders rows
// naming one media id - because T4.6 removed the only route that used to
// produce it. Building it here is the point: the rule is about the store, so a
// row a route no longer writes still has to be answered.

// png is the smallest image a blob can carry. These tests read the bytes back
// to prove a picture still opens; they never decode one.
var png = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// eraseFix is one store, one blob store and one deletion engine over them.
type eraseFix struct {
	t      *testing.T
	db     *sqlite.DB
	media  *mediastore.Store
	index  mediastore.BlobIndex
	jobs   job.Store
	eng    *privacy.Engine
	eraser *erase.Eraser
	runner *job.Runner
	dir    string
}

func openErasure(t *testing.T) *eraseFix {
	t.Helper()
	return openErasureWith(t, openStoreSchema(t))
}

// openErasureWith wires the rig onto a schema the caller already built, so the
// cascade-free database below runs the very same engine. Keel's identity store
// opens here too, because the account deletion's rows target reaches the
// sign-in, session and identity rows, and a table that is not there is a
// delete that fails.
func openErasureWith(t *testing.T, db *sqlite.DB) *eraseFix {
	t.Helper()
	index, err := mediasql.Open(t.Context(), mediasql.Config{DB: db})
	if err != nil {
		t.Fatalf("media index: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "media")
	media, err := mediastore.Open(t.Context(), mediastore.Config{
		Dir:   dir,
		Index: index,
	})
	if err != nil {
		t.Fatalf("media store: %v", err)
	}
	if _, err := identitysql.Open(t.Context(), identitysql.Config{DB: db}); err != nil {
		t.Fatalf("identity store: %v", err)
	}
	jobStore, err := jobsql.Open(t.Context(), jobsql.Config{DB: db})
	if err != nil {
		t.Fatalf("job store: %v", err)
	}
	eng := newEngine(t, db, realBlobs{media: media, index: index}, newMemory(nil))
	eraser, err := erase.New(eng.Source, erase.Config{Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("eraser: %v", err)
	}
	return &eraseFix{
		t:      t,
		db:     db,
		media:  media,
		index:  index,
		jobs:   jobStore,
		eng:    eng,
		eraser: eraser,
		dir:    dir,
		runner: openRunner(t, db, map[string]job.Kind{
			privacy.KindName: eng.Kind(),
			erase.KindName:   eraser.Kind(),
		}),
	}
}

// openStoreSchema lands the Dresscode schema in a temp file.
func openStoreSchema(t *testing.T) *sqlite.DB {
	t.Helper()
	db := openDB(t)
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatalf("store: %v", err)
	}
	return db
}

// noCascadePattern is a column-level foreign key clause, which is the only
// shape internal/store/migrations writes. Stripping it leaves the landed
// columns and types exactly as they are and takes the cascade away, so a
// deletion that clears the child rows on this schema clears them because this
// package said so.
var noCascadePattern = regexp.MustCompile(`(?i)\s+REFERENCES\s+\w+\s*\(\s*\w+\s*\)(\s+ON\s+DELETE\s+(CASCADE|SET\s+NULL|SET\s+DEFAULT|RESTRICT|NO\s+ACTION))?`)

// openNoCascade copies the landed schema into a fresh file without its foreign
// keys. assertSameColumns pins that the copy holds the same tables and columns,
// so this rig cannot quietly drop out of step with the migrations.
func openNoCascade(t *testing.T) *eraseFix {
	t.Helper()
	src := openStoreSchema(t)
	db := openDB(t)
	for _, table := range schemaTables(t, src) {
		var ddl string
		err := src.Reader().QueryRowContext(t.Context(),
			`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&ddl)
		if err != nil {
			t.Fatalf("the ddl of %s: %v", table, err)
		}
		if _, err := db.Writer().ExecContext(t.Context(), noCascadePattern.ReplaceAllString(ddl, "")); err != nil {
			t.Fatalf("rebuild %s: %v", table, err)
		}
	}
	assertSameColumns(t, src, db)
	return openErasureWith(t, db)
}

// assertSameColumns fails when two databases hold a different table and column
// set. It keeps the cascade-free copy honest about what it copies.
func assertSameColumns(t *testing.T, src, dst *sqlite.DB) {
	t.Helper()
	want, got := columnSet(t, src), columnSet(t, dst)
	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Fatalf("the cascade-free copy holds %v, want %v", got, want)
	}
}

func columnSet(t *testing.T, db *sqlite.DB) []string {
	t.Helper()
	var out []string
	for _, table := range schemaTables(t, db) {
		_, cols := textColumns(t, db, table)
		for _, column := range cols {
			out = append(out, table+"."+column)
		}
		ints, err := db.Reader().QueryContext(t.Context(),
			`SELECT name FROM pragma_table_info(?) WHERE upper(type) NOT IN ('TEXT', 'BLOB', '')`, table)
		if err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		for ints.Next() {
			var name string
			if err := ints.Scan(&name); err != nil {
				ints.Close()
				t.Fatalf("columns of %s: %v", table, err)
			}
			out = append(out, table+"."+name)
		}
		if err := ints.Err(); err != nil {
			ints.Close()
			t.Fatalf("columns of %s: %v", table, err)
		}
		ints.Close()
	}
	return out
}

// keep writes one picture into the blob store under a minted id, the way the
// intake and the render stage do, and answers it.
func (fx *eraseFix) keep(owner, group string) string {
	t := fx.t
	t.Helper()
	mediaID, err := id.New()
	if err != nil {
		t.Fatalf("mint the media id: %v", err)
	}
	if err := fx.media.PersistWithID(t.Context(), mediaID, bytes.NewReader(png), mediastore.Put{
		ContentType: "image/png",
		Owner:       owner,
		Group:       group,
		Visibility:  mediastore.Private,
	}); err != nil {
		t.Fatalf("keep %s: %v", mediaID, err)
	}
	return mediaID
}

// read is the store's own read path: the bytes, and whether the picture is
// there at all. This is what "the other one's picture is reading" means in the
// charter's second clause.
func (fx *eraseFix) read(mediaID string) ([]byte, bool) {
	t := fx.t
	t.Helper()
	rc, _, err := fx.media.Open(t.Context(), mediaID)
	if err != nil {
		if !errIs(err, mediastore.ErrNotFound) {
			t.Fatalf("open %s: %v", mediaID, err)
		}
		return nil, false
	}
	defer rc.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(rc); err != nil {
		t.Fatalf("read %s: %v", mediaID, err)
	}
	return out.Bytes(), true
}

// indexed reports whether the media index still holds a row for the id. The
// row and the bytes go together, and a test that only read one of them would
// miss half of what deletion promises.
func (fx *eraseFix) indexed(mediaID string) bool {
	t := fx.t
	t.Helper()
	var n int
	err := fx.db.Reader().QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM media WHERE id = ?`, mediaID).Scan(&n)
	if err != nil {
		t.Fatalf("the media row of %s: %v", mediaID, err)
	}
	if n > 1 {
		t.Fatalf("%s has %d media rows", mediaID, n)
	}
	return n == 1
}

func errIs(err, want error) bool { return errors.Is(err, want) }

// landLook writes one finished look's job row through Keel's own store, so the
// row is the shape the runner leaves behind rather than an imitation of it.
// The result is internal/pipeline.Look's own JSON - the finished look, its
// shown render's media id and the shop list - and the progress is the check
// frame's shape, the one internal/server/samples.go mirrors for a playback.
//
// A paid look and a sample story write the same kind with the same shape, so
// this one function lands both: root is the look id when the look was kept,
// and an id no looks row holds when the keep failed, which is the row the
// first clause is about.
func (fx *eraseFix) landLook(root string, progress job.Progress, shown string) {
	t := fx.t
	t.Helper()
	look := pipeline.Look{
		Flags: []domain.Criterion{{ID: "earring", Suggestion: "Add a jhumka."}},
		Issue: pipeline.Issue{ID: "balance", Label: "Top-heavy"},
		FixID: "family-gold-jhumkas",
		Shown: &domain.Render{MediaID: shown, FixID: "family-gold-jhumkas"},
		Words: "gold jhumkas and a red lip",
		Products: []domain.Product{{
			ID: "gold-jhumkas", Category: "earrings", Name: "Gold jhumkas",
			Fidelity: domain.ExactProduct, Origin: domain.OriginCatalog,
		}},
	}
	data, err := json.Marshal(look)
	if err != nil {
		t.Fatalf("the look's result: %v", err)
	}
	fx.land(job.Record{
		ID: root, Kind: "look", Status: job.StatusDone, Attempt: 1, RootID: root,
		Progress: progress, Data: data, UpdatedAt: time.Unix(1, 0).UTC(),
	})
}

func (fx *eraseFix) land(rec job.Record) {
	t := fx.t
	t.Helper()
	if err := fx.jobs.Create(t.Context(), rec); err != nil {
		t.Fatalf("land the job row: %v", err)
	}
}

// checkFrame is the pipeline's check event for one render: the fix, that it was
// shown, the picture, and the words the check said.
func checkFrame(mediaID string) job.Progress {
	detail, err := json.Marshal(map[string]any{
		"fix": "family-gold-jhumkas", "shown": true, "media_id": mediaID,
		"words": "gold jhumkas and a red lip",
	})
	if err != nil {
		panic(err)
	}
	return job.Progress{Stage: "check", Detail: detail}
}

// lookPlan is one finished look as the store holds it after materialise.
type lookPlan struct {
	id       string
	user     string
	selfie   string
	hash     string
	renders  []string
	snapshot bool
}

// seedLook writes the rows a finished look leaves: the look, one render per
// picture with its check and its vocabulary, the flag and the shop join, and
// the snapshot the paid path keeps when it starts.
func (fx *eraseFix) seedLook(p lookPlan) {
	t := fx.t
	t.Helper()
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := fx.db.Writer().ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed the look %s: %v", p.id, err)
		}
	}
	exec(`INSERT INTO looks
		(id, user_id, selfie_id, photo_hash, occasion, fix, runner_up, verdict, created_at)
		VALUES (?, ?, ?, ?, 'family-function', 'family-gold-jhumkas', 'family-red-lip', ?, 1)`,
		p.id, p.user, p.selfie, p.hash, string(domain.VerdictKept))
	exec(`INSERT INTO look_flags (look_id, user_id, position, criterion_id) VALUES (?, ?, 0, 'earring')`,
		p.id, p.user)
	for i, mediaID := range p.renders {
		render := p.id + fmt.Sprintf("-render-%d", i)
		exec(`INSERT INTO renders (id, look_id, user_id, media_id, fix_id, position, discarded)
			VALUES (?, ?, ?, ?, 'family-gold-jhumkas', ?, 0)`, render, p.id, p.user, mediaID, i)
		exec(`INSERT INTO render_checks (render_id, look_id, user_id, change_made, same_person, criterion_met)
			VALUES (?, ?, ?, 1, 1, 1)`, render, p.id, p.user)
		exec(`INSERT INTO render_check_attributes (render_id, user_id, attribute_id, value)
			VALUES (?, ?, 'jewellery', 'jhumkas')`, render, p.user)
	}
	exec(`INSERT INTO products (id, user_id, category, name, fidelity, origin)
		VALUES (?, ?, 'earrings', 'Gold jhumkas', ?, ?)`,
		p.id+"-product", p.user, string(domain.ExactProduct), string(domain.OriginUser))
	exec(`INSERT INTO look_products
		(look_id, user_id, position, product_id, category, name, fidelity, origin)
		VALUES (?, ?, 0, 'gold-jhumkas', 'earrings', 'Gold jhumkas', ?, ?)`,
		p.id, p.user, string(domain.ExactProduct), string(domain.OriginCatalog))
	if p.snapshot {
		snap, err := json.Marshal(map[string]any{
			"occasion": "family-function", "user": p.user, "selfie": p.selfie, "hash": p.hash,
		})
		if err != nil {
			t.Fatalf("the snapshot: %v", err)
		}
		exec(`INSERT INTO look_snapshots (root_id, user_id, snapshot) VALUES (?, ?, ?)`,
			p.id, p.user, string(snap))
	}
}

// deleteLook runs one look deletion through the route's own entry point and
// waits for it to finish.
func (fx *eraseFix) deleteLook(user, lookID string) job.Record {
	t := fx.t
	t.Helper()
	jobID, err := fx.eng.StartLook(t.Context(), fx.runner, user, lookID)
	if err != nil {
		t.Fatalf("start the look deletion %s: %v", lookID, err)
	}
	rec := waitJob(t, fx.runner, jobID, job.StatusDone)
	assertErase(t, rec, []string{privacy.TargetLook, privacy.TargetFiles})
	return rec
}

// ownedIDs lists the ids in one account's blob list, the way deletion sees it.
func (fx *eraseFix) ownedIDs(owner string) []string {
	t := fx.t
	t.Helper()
	blobs, err := fx.index.Blobs(t.Context())
	if err != nil {
		t.Fatalf("the blob list: %v", err)
	}
	var out []string
	for _, blob := range blobs {
		if blob.Owner == owner {
			out = append(out, blob.ID)
		}
	}
	return out
}

// eraseAccount runs the account deletion's own erasure: the target list
// Engine.Source builds for the user, driven by Keel's eraser as a job. That is
// what identity.Service.Delete starts once it has checked the code, and the
// code check is TestAccountDeletionRestartsMidway's subject. What this rig
// holds is the store the targets sweep.
func (fx *eraseFix) eraseAccount(user string) {
	t := fx.t
	t.Helper()
	targets, err := fx.eng.Source(t.Context(), user)
	if err != nil {
		t.Fatalf("the account's targets: %v", err)
	}
	jobID, err := fx.eraser.Start(t.Context(), fx.runner, user, targets)
	if err != nil {
		t.Fatalf("start the account erasure: %v", err)
	}
	waitJob(t, fx.runner, jobID, job.StatusDone)
	rep, err := fx.eraser.Inspect(t.Context(), fx.runner, jobID)
	if err != nil {
		t.Fatalf("the erasure ledger: %v", err)
	}
	if rep.Ref != user || !rep.Complete() {
		t.Fatalf("the erasure of %s stuck on %v", user, rep.Stuck)
	}
	got := append([]string(nil), rep.Confirmed...)
	slices.Sort(got)
	want := []string{privacy.TargetFiles, privacy.TargetMemory, privacy.TargetRows}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("the erasure confirmed %v, want %v", got, want)
	}
}

// deleteAccountRows runs one target of the account erasure on its own, because
// a real erasure runs the three at once and one of them reads what another is
// emptying. A test that wants an order asks for one target.
func (fx *eraseFix) deleteAccountRows(user string) {
	t := fx.t
	t.Helper()
	targets, err := fx.eng.Source(t.Context(), user)
	if err != nil {
		t.Fatalf("the account's targets: %v", err)
	}
	var rows erase.Target
	for _, target := range targets {
		if target.Name() == privacy.TargetRows {
			rows = target
		}
	}
	if rows == nil {
		t.Fatal("the account's target list holds no rows target")
	}
	if err := rows.Delete(t.Context()); err != nil && !errors.Is(err, erase.ErrGone) {
		t.Fatalf("the rows target: %v", err)
	}
}

// seedRecord lands the two rows an account holds beside its looks that the
// erasure has to reach: the analysis cache's answer for a photo hash, and
// Keel's sign-in, session and identity rows.
func (fx *eraseFix) seedRecord(user, hash string) {
	t := fx.t
	t.Helper()
	seedIdentity(t, fx.db, user)
	if _, err := fx.db.Writer().ExecContext(t.Context(),
		`INSERT INTO analysis_cache (user_id, photo_hash, task, face_shape)
		 VALUES (?, ?, 'face-attr-analysis', 'oval')`, user, hash); err != nil {
		t.Fatalf("seed the analysis row of %s: %v", user, err)
	}
}

// landPhotoDeletion lands the ledger row of a photo deletion this account
// finished: a row of the deletion kind whose snapshot names the user and the
// picture that went, rooted at no look at all. The sweep that reads the
// account's looks cannot see it; the sweep that reads the chain's text can,
// and this is the row that proves it reaches.
func (fx *eraseFix) landPhotoDeletion(user, photo string) {
	t := fx.t
	t.Helper()
	detail, err := json.Marshal(map[string]any{"user": user, "photo": photo})
	if err != nil {
		t.Fatalf("the deletion snapshot: %v", err)
	}
	root := user + "-photo-deletion"
	fx.land(job.Record{
		ID: root, Kind: privacy.KindName, Status: job.StatusDone, Attempt: 1, RootID: root,
		Progress:  job.Progress{Stage: "delete", Detail: detail},
		UpdatedAt: time.Unix(1, 0).UTC(),
	})
}

// ledgerKind reports whether one job row is an erasure's own record. Keel keeps
// a finished erasure readable because the job id it carries is the capability
// for the deletion stream (docs/api.md, the deletion section), and its snapshot
// holds the minted user id, the target names and nothing else. The assertions
// below are what "nothing else" means.
func (fx *eraseFix) ledgerKind(jobID string) bool {
	t := fx.t
	t.Helper()
	var kind string
	err := fx.db.Reader().QueryRowContext(t.Context(),
		`SELECT kind FROM jobs WHERE id = ?`, jobID).Scan(&kind)
	if err != nil {
		t.Fatalf("the kind of the job row %s: %v", jobID, err)
	}
	return kind == erase.KindName || kind == identity.DeletionKindName
}

// ---------------------------------------------------------------------------
// Item 2: a blob goes only when no surviving row names it

func TestSharedBlobStandsWhileAnotherLookShowsIt(t *testing.T) {
	t.Parallel()
	fx := openErasure(t)
	const user = "user-a"
	selfie := fx.keep(user, "selfie")
	shared := fx.keep(user, "render/"+user)
	first := fx.keep(user, "render/"+user)
	second := fx.keep(user, "render/"+user)
	fx.seedLook(lookPlan{id: user + "-look-1", user: user, selfie: selfie, hash: user + "-hash",
		renders: []string{shared, first}, snapshot: true})
	fx.seedLook(lookPlan{id: user + "-look-2", user: user, selfie: selfie, hash: user + "-hash",
		renders: []string{shared, second}, snapshot: true})
	fx.landLook(user+"-look-1", checkFrame(shared), shared)
	fx.landLook(user+"-look-2", checkFrame(second), second)

	fx.deleteLook(user, user+"-look-1")

	// The count refuses what something else still names: the other look's
	// picture is still there and still opens to the bytes it was given.
	if bytes, ok := fx.read(shared); !ok || !bytesEqual(bytes, png) {
		t.Fatalf("the shared picture went with a look that was not the last to name it: read %v", ok)
	}
	if !fx.indexed(shared) {
		t.Fatal("the shared picture lost its index row")
	}
	// The count erases what nothing else names: the render only this look
	// showed is gone from the disk and from the index.
	if _, ok := fx.read(first); ok {
		t.Fatal("the render only the deleted look named is still readable")
	}
	if fx.indexed(first) {
		t.Fatal("the render only the deleted look named kept its index row")
	}
	// The selfie is not a render: no look deletion names it, so it stays.
	if _, ok := fx.read(selfie); !ok {
		t.Fatal("a look deletion erased the selfie")
	}
	// The deleted look's own rows are gone, and the other look's stay.
	if count(t, fx.db, store.TableLooks, user) != 1 || !fx.hasLookRow(user+"-look-2") {
		t.Fatalf("the deleted look left %d looks", count(t, fx.db, store.TableLooks, user))
	}
	if fx.hasRender(user + "-look-1") {
		t.Fatal("the deleted look left its renders row")
	}
	if !fx.hasRender(user + "-look-2") {
		t.Fatal("the surviving look lost its renders row")
	}
	assertNoDanglingRows(t, fx.db)

	// And again, in the other direction, when the last look naming the
	// shared picture goes: the erasure takes it, because nothing names it.
	fx.deleteLook(user, user+"-look-2")
	if _, ok := fx.read(shared); ok {
		t.Fatal("the last look naming the shared picture left it behind")
	}
	if fx.indexed(shared) {
		t.Fatal("the shared picture kept its index row after the last look went")
	}
	if _, ok := fx.read(second); ok {
		t.Fatal("the second look's own render survived")
	}
	if _, ok := fx.read(selfie); !ok {
		t.Fatal("the selfie went with the last look that used it")
	}
	assertNoDanglingRows(t, fx.db)
	if got := fx.ownedIDs(user); len(got) != 1 || got[0] != selfie {
		t.Fatalf("the account's blob list is %v, want the selfie alone", got)
	}
}

func (fx *eraseFix) hasLookRow(lookID string) bool {
	t := fx.t
	t.Helper()
	var n int
	err := fx.db.Reader().QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM looks WHERE id = ?`, lookID).Scan(&n)
	if err != nil {
		t.Fatalf("the look row of %s: %v", lookID, err)
	}
	return n == 1
}

func (fx *eraseFix) hasRender(lookID string) bool {
	t := fx.t
	t.Helper()
	var n int
	err := fx.db.Reader().QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM renders WHERE look_id = ?`, lookID).Scan(&n)
	if err != nil {
		t.Fatalf("the renders of %s: %v", lookID, err)
	}
	return n > 0
}

// TestPhotoDeletionErasesWhatItsOwnLooksNamed covers the order the photo
// deletion runs in: its targets are files then looks, so the reference count
// has to be told which looks this same erasure is about to remove, or every
// selfie would be refused by the live looks it is about to delete.
func TestPhotoDeletionErasesWhatItsOwnLooksNamed(t *testing.T) {
	t.Parallel()
	fx := openErasure(t)
	const user, other = "user-a", "user-b"
	stranger := fx.keep(other, "selfie")
	selfie := fx.keep(user, "selfie")
	render := fx.keep(user, "render/"+user)
	fx.seedLook(lookPlan{id: user + "-look-1", user: user, selfie: selfie, hash: user + "-hash",
		renders: []string{render}, snapshot: true})
	fx.seedLook(lookPlan{id: user + "-look-2", user: user, selfie: selfie, hash: user + "-hash",
		renders: []string{render}, snapshot: true})
	fx.seedLook(lookPlan{id: other + "-look", user: other, selfie: stranger, hash: other + "-hash",
		renders: []string{fx.keep(other, "render/"+other)}, snapshot: true})

	jobID, err := fx.eng.StartPhoto(t.Context(), fx.runner, user, selfie)
	if err != nil {
		t.Fatalf("start the photo deletion: %v", err)
	}
	rec := waitJob(t, fx.runner, jobID, job.StatusDone)
	assertErase(t, rec, []string{privacy.TargetFiles, privacy.TargetLooks})

	for _, mediaID := range []string{selfie, render} {
		if _, ok := fx.read(mediaID); ok {
			t.Fatalf("the photo deletion left %s readable", mediaID)
		}
		if fx.indexed(mediaID) {
			t.Fatalf("the photo deletion left the media row of %s", mediaID)
		}
	}
	if count(t, fx.db, store.TableLooks, user) != 0 {
		t.Fatal("the photo deletion left a look that could not be shown")
	}
	// The other account is untouched, rows and bytes both.
	if _, ok := fx.read(stranger); !ok {
		t.Fatal("the photo deletion reached into another account")
	}
	if count(t, fx.db, store.TableLooks, other) != 1 {
		t.Fatal("the photo deletion removed another account's look")
	}
	assertNoDanglingRows(t, fx.db)
}

// ---------------------------------------------------------------------------
// Item 2's second half: a single look deletion leaves no row naming a gone id

func TestSingleLookDeletionLeavesNoRowNamingAGoneMedia(t *testing.T) {
	t.Parallel()
	fx := openErasure(t)
	const user = "user-a"
	selfie := fx.keep(user, "selfie")
	render := fx.keep(user, "render/"+user)
	fx.seedLook(lookPlan{id: user + "-look-1", user: user, selfie: selfie, hash: user + "-hash",
		renders: []string{render}, snapshot: true})
	fx.seedLook(lookPlan{id: user + "-look-2", user: user, selfie: selfie, hash: user + "-hash",
		renders: []string{fx.keep(user, "render/"+user)}, snapshot: true})
	fx.landLook(user+"-look-1", checkFrame(render), render)

	fx.deleteLook(user, user+"-look-1")

	// The render's row, its check, the check's vocabulary, the flag, the
	// shop join, the snapshot and the job chain are all gone, and the
	// picture they named is gone with them. Nothing left names either.
	if _, ok := fx.read(render); ok {
		t.Fatal("the deleted look's render is still readable")
	}
	assertNoDanglingRows(t, fx.db)
	if n := fx.tableCount("jobs"); n != 1 {
		t.Fatalf("the surviving look's chain left %d job rows, want its own 1", n)
	}
	if !fx.hasLookRow(user+"-look-2") || !fx.hasRender(user+"-look-2") {
		t.Fatal("the single look deletion reached the other look")
	}
	if _, ok := fx.read(selfie); !ok {
		t.Fatal("the selfie went with one look")
	}
}

// TestLookDeletionSweepDoesNotLeanOnTheForeignKeyCascade is the same deletion
// on a schema that is the landed one with its foreign keys stripped. The
// cascade is what made a bare DELETE FROM looks clear the children by
// accident; on this schema only this package's sweep can, and it must.
func TestLookDeletionSweepDoesNotLeanOnTheForeignKeyCascade(t *testing.T) {
	t.Parallel()
	fx := openNoCascade(t)
	const user = "user-a"
	selfie := fx.keep(user, "selfie")
	render := fx.keep(user, "render/"+user)
	fx.seedLook(lookPlan{id: user + "-look-1", user: user, selfie: selfie, hash: user + "-hash",
		renders: []string{render}, snapshot: true})
	fx.landLook(user+"-look-1", checkFrame(render), render)

	fx.deleteLook(user, user+"-look-1")

	for _, table := range []string{"renders", "render_checks", "render_check_attributes",
		"look_flags", "look_products", "look_snapshots"} {
		if got := fx.tableWhere(table, `user_id = ?`, user); got != 0 {
			t.Fatalf("%s kept %d rows of the deleted look with no cascade to take them", table, got)
		}
	}
	if got := fx.countKind("look"); got != 0 {
		t.Fatalf("jobs kept %d look chains with no cascade to take them: %v", got, fx.jobChains())
	}
	if got := fx.countKind("deletion"); got != 1 {
		t.Fatalf("the deletion's own ledger row is %d, want the one that just ran: %v",
			got, fx.jobChains())
	}
	if got := fx.tableWhere("looks", `user_id = ?`, user); got != 0 {
		t.Fatalf("looks kept %d rows", got)
	}
	if _, ok := fx.read(render); ok {
		t.Fatal("the sweep left the render it was made for readable")
	}
}

// ---------------------------------------------------------------------------
// Item 1: the account's rows in Keel's job store

// TestAccountErasureReachesSampleAndPaidLookChains is the charter's first
// clause. An account that opened samples and ran a paid look goes through the
// erasure the account deletion runs, and after it no row anywhere in the file
// names anything the erase can reach: not the look ids, not the pictures, not
// the photo hash, not the address, not the analysis answer.
//
// The two chains are reached two different ways, and that is the finding. A
// sample's row and a paid look's row are the same kind in the same shape - the
// sample route lands a born-finished row of the look kind - and neither carries
// the user id in its text, so what reaches them is the looks row the route
// leaves beside them. The finished photo deletion's ledger is the opposite: it
// names the user in its snapshot and no look anywhere, so the looks sweep
// cannot see it and the text sweep takes it.
func TestAccountErasureReachesSampleAndPaidLookChains(t *testing.T) {
	t.Parallel()
	fx := openErasure(t)
	const user, other = "user-a", "user-b"
	selfie := fx.keep(user, "selfie")
	paidRender := fx.keep(user, "render/"+user)
	sampleRender := fx.keep(user, "render/"+user)
	erased := fx.keep(user, "selfie")
	paid, sample := user+"-paid", user+"-sample"
	fx.seedLook(lookPlan{id: paid, user: user, selfie: selfie, hash: user + "-hash",
		renders: []string{paidRender}, snapshot: true})
	fx.seedLook(lookPlan{id: sample, user: user, selfie: selfie, hash: user + "-hash",
		renders: []string{sampleRender}})
	fx.seedRecord(user, user+"-hash")
	fx.landLook(paid, checkFrame(paidRender), paidRender)
	fx.landLook(sample, checkFrame(sampleRender), sampleRender)
	fx.landPhotoDeletion(user, erased)
	// The paid look's second attempt: the retry's own row, and the error
	// detail that names the picture it was making when it went wrong. A
	// sweep of a chain takes the attempts with it.
	fx.land(job.Record{
		ID: paid + "-retry", Kind: "look", Status: job.StatusError, Attempt: 2,
		ParentID: paid, RootID: paid, Err: errors.New("the render went wrong"),
		Detail:    json.RawMessage(`{"media_id":"` + paidRender + `"}`),
		UpdatedAt: time.Unix(2, 0).UTC(),
	})

	// A second account with the same shape, to be left whole.
	otherSelfie := fx.keep(other, "selfie")
	otherRender := fx.keep(other, "render/"+other)
	fx.seedLook(lookPlan{id: other + "-look", user: other, selfie: otherSelfie, hash: other + "-hash",
		renders: []string{otherRender}, snapshot: true})
	fx.seedRecord(other, other+"-hash")
	fx.landLook(other+"-look", checkFrame(otherRender), otherRender)

	fx.eraseAccount(user)

	// The store-wide read: every TEXT and BLOB column of every table, asked
	// whether it still carries the token, rather than only the tables this
	// package's deletion code happens to mention.
	for _, token := range []string{
		paid, sample, selfie, paidRender, sampleRender, erased,
		user + "-hash", user + "@example.com",
	} {
		if hits := rowsNaming(t, fx.db, token); len(hits) != 0 {
			t.Fatalf("%d row(s) still name %s: %v", len(hits), token, hits)
		}
	}
	// What survives all three sweeps is the erasure's own ledger row, and it
	// is a record of this erase rather than a piece of the person's data.
	if hits := rowsNaming(t, fx.db, user); len(hits) != 1 ||
		!strings.HasPrefix(hits[0], "jobs=") || !fx.ledgerKind(strings.TrimPrefix(hits[0], "jobs=")) {
		t.Fatalf("rows naming the erased account: %v — %s", hits, fx.dumpJobs())
	}
	// The account's rows, in every table that holds one, including the four
	// Keel's identity store owns.
	for _, cleared := range []struct{ table, where, arg string }{
		{"looks", `user_id = ?`, user},
		{"renders", `user_id = ?`, user},
		{"render_checks", `user_id = ?`, user},
		{"render_check_attributes", `user_id = ?`, user},
		{"look_flags", `user_id = ?`, user},
		{"look_products", `user_id = ?`, user},
		{"look_snapshots", `user_id = ?`, user},
		{"products", `user_id = ?`, user},
		{"analysis_cache", `user_id = ?`, user},
		{"opt_ins", `user_id = ?`, user},
		{"history_switch", `user_id = ?`, user},
		{"users", `id = ?`, user},
		{"sessions", `user_id = ?`, user},
		{"identities", `user_id = ?`, user},
		{"sign_in_codes", `requesting_session = ?`, user + "-session"},
	} {
		if got := fx.tableWhere(cleared.table, cleared.where, cleared.arg); got != 0 {
			t.Fatalf("the account erasure left %d %s rows", got, cleared.table)
		}
	}
	// Job chains of the account's own kinds: the two looks, the retry's row
	// beside its root, and the finished photo deletion.
	if got := fx.countKind("look"); got != 1 {
		t.Fatalf("the job table holds %d look chains, want the other account's one: %v",
			got, fx.jobChains())
	}
	if got := fx.countKind("deletion"); got != 0 {
		t.Fatalf("the job table holds %d deletion chains, want none: %v", got, fx.jobChains())
	}
	if got := fx.tableCount("jobs"); got != 2 {
		t.Fatalf("the job table holds %d rows, want the other account's chain and this erasure's ledger: %s",
			got, fx.dumpJobs())
	}
	// Every byte the account held is unreadable and unindexed.
	for _, mediaID := range []string{selfie, paidRender, sampleRender, erased} {
		if _, ok := fx.read(mediaID); ok {
			t.Fatalf("%s is still readable after the account erasure", mediaID)
		}
		if fx.indexed(mediaID) {
			t.Fatalf("%s kept its media row after the account erasure", mediaID)
		}
	}
	if got := fx.ownedIDs(user); len(got) != 0 {
		t.Fatalf("the blob list still holds %v for the erased account", got)
	}
	// The other account is untouched: bytes, rows and its own chain.
	for _, mediaID := range []string{otherSelfie, otherRender} {
		if _, ok := fx.read(mediaID); !ok {
			t.Fatalf("the account erasure took the bytes of %s from another account", mediaID)
		}
	}
	if got := fx.tableWhere("looks", `user_id = ?`, other); got != 1 {
		t.Fatalf("the other account holds %d looks", got)
	}
	if got := fx.tableWhere("analysis_cache", `user_id = ?`, other); got != 1 {
		t.Fatalf("the other account's analysis answer is gone")
	}
	if got := fx.tableWhere("users", `id = ?`, other); got != 1 {
		t.Fatalf("the other account's user row is gone")
	}
	if got := fx.tableWhere("jobs", `root_id = ?`, other+"-look"); got != 1 {
		t.Fatalf("the other account's look chain is gone: %s", fx.dumpJobs())
	}
	assertNoDanglingRows(t, fx.db)
}

// TestUnrecordedChainGoesWithThePicturesItNamed is the third sweep: a finished
// look whose looks row never landed. The sample route lands the story first and
// keeps the look after it, so a failed keep leaves a chain rooted at no look,
// naming no user id, and tying itself to the account only through the owner of
// the picture it kept.
//
// The rows target reads that owner inside its own transaction, before the rows
// it names go. A real erasure runs the files target beside it, and the files
// target empties the very list - which is the order jobrows.go names as the
// honest remainder. This test asks for the one target, so the read it is
// proving is not a coin flip.
func TestUnrecordedChainGoesWithThePicturesItNamed(t *testing.T) {
	t.Parallel()
	fx := openErasure(t)
	const user, other = "user-a", "user-b"
	kept := fx.keep(user, "render/"+user)
	root, err := id.New()
	if err != nil {
		t.Fatalf("mint the root: %v", err)
	}
	fx.landLook(root, checkFrame(kept), kept)
	// The same picture shown to a second account, whose looks row did land.
	// That chain is the other person's record of their own look, and this
	// erase is not where a route that shared one picture between two
	// accounts gets untangled.
	otherSelfie := fx.keep(other, "selfie")
	fx.seedLook(lookPlan{id: other + "-look", user: other, selfie: otherSelfie, hash: other + "-hash",
		renders: []string{kept}})
	fx.landLook(other+"-look", checkFrame(kept), kept)

	fx.deleteAccountRows(user)

	if got := fx.tableWhere("jobs", `root_id = ?`, root); got != 0 {
		t.Fatalf("the unrecorded chain of %s survived: %s", root, fx.dumpJobs())
	}
	if got := fx.tableWhere("jobs", `root_id = ?`, other+"-look"); got != 1 {
		t.Fatalf("the erase reached the other account's chain: %s", fx.dumpJobs())
	}
	if !fx.hasLookRow(other + "-look") {
		t.Fatal("the erase removed the look that held the shared picture")
	}
	// The bytes of a picture another account still shows are not this
	// target's to take, and the rows target holds no blob at all.
	if _, ok := fx.read(kept); !ok {
		t.Fatal("the rows target erased a picture another look still shows")
	}
	if _, ok := fx.read(otherSelfie); !ok {
		t.Fatal("the rows target erased another account's selfie")
	}
	assertNoDanglingRows(t, fx.db)
}

// jobChains names every job row left, as kind and root id, sorted. A test that
// says which chains survive can tell the look's own chain from the ledger of
// the deletion that took it.
func (fx *eraseFix) jobChains() []string {
	t := fx.t
	t.Helper()
	rows, err := fx.db.Reader().QueryContext(t.Context(), `SELECT kind, root_id FROM jobs ORDER BY kind, root_id`)
	if err != nil {
		t.Fatalf("the job rows: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var kind, root string
		if err := rows.Scan(&kind, &root); err != nil {
			t.Fatalf("the job rows: %v", err)
		}
		out = append(out, kind+"="+root)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("the job rows: %v", err)
	}
	return out
}

// assertLookChainsGone fails when a chain of the look kind is left for one of
// the given roots.
func (fx *eraseFix) countKind(kind string) int {
	t := fx.t
	t.Helper()
	n := 0
	for _, chain := range fx.jobChains() {
		if strings.HasPrefix(chain, kind+"=") {
			n++
		}
	}
	return n
}

func (fx *eraseFix) tableCount(table string) int {
	t := fx.t
	t.Helper()
	return fx.tableWhere(table, `1`)
}

// dumpJobs names the job rows that are left, for a failure message.
func (fx *eraseFix) dumpJobs() string {
	t := fx.t
	t.Helper()
	rows, err := fx.db.Reader().QueryContext(t.Context(),
		`SELECT id, kind, status, root_id, parent_id FROM jobs ORDER BY id`)
	if err != nil {
		return "the job read failed: " + err.Error()
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, kind, status, root, parent sql.NullString
		if err := rows.Scan(&id, &kind, &status, &root, &parent); err != nil {
			return "the job scan failed: " + err.Error()
		}
		out = append(out, fmt.Sprintf("{id %s kind %s status %s root %s parent %s}",
			id.String, kind.String, status.String, root.String, parent.String))
	}
	if err := rows.Err(); err != nil {
		return "the job rows failed: " + err.Error()
	}
	return strings.Join(out, " ")
}

func (fx *eraseFix) tableWhere(table, predicate string, args ...any) int {
	t := fx.t
	t.Helper()
	var n int
	query := `SELECT COUNT(*) FROM "` + table + `" WHERE ` + predicate
	if err := fx.db.Reader().QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// assertNoDanglingRows is the store-wide shape of the third clause: no row
// names a media id the blob store no longer holds, and no child row names a
// render or a look that is gone.
func assertNoDanglingRows(t *testing.T, db *sqlite.DB) {
	t.Helper()
	ctx := t.Context()
	checks := []struct {
		what  string
		query string
	}{
		{"a renders row naming a media id that is gone",
			`SELECT COUNT(*) FROM renders r LEFT JOIN media m ON m.id = r.media_id WHERE m.id IS NULL`},
		{"a looks row naming a selfie that is gone",
			`SELECT COUNT(*) FROM looks l LEFT JOIN media m ON m.id = l.selfie_id WHERE m.id IS NULL`},
		{"a render_checks row naming a render that is gone",
			`SELECT COUNT(*) FROM render_checks c WHERE c.render_id NOT IN (SELECT id FROM renders)`},
		{"a render_check_attributes row naming a check that is gone",
			`SELECT COUNT(*) FROM render_check_attributes a
			 WHERE a.render_id NOT IN (SELECT render_id FROM render_checks)`},
		{"a child row of a deleted look",
			`SELECT COUNT(*) FROM renders WHERE look_id NOT IN (SELECT id FROM looks)`},
		{"a flag row of a deleted look",
			`SELECT COUNT(*) FROM look_flags WHERE look_id NOT IN (SELECT id FROM looks)`},
		{"a shop row of a deleted look",
			`SELECT COUNT(*) FROM look_products WHERE look_id NOT IN (SELECT id FROM looks)`},
		{"a snapshot of a deleted look",
			`SELECT COUNT(*) FROM look_snapshots WHERE root_id NOT IN (SELECT id FROM looks)`},
		{"a job chain of a look no account holds",
			`SELECT COUNT(*) FROM jobs j WHERE j.kind = 'look'
			 AND j.root_id NOT IN (SELECT id FROM looks)`},
	}
	for _, check := range checks {
		var n int
		if err := db.Reader().QueryRowContext(ctx, check.query).Scan(&n); err != nil {
			t.Fatalf("%s: %v", check.what, err)
		}
		if n != 0 {
			t.Fatalf("found %d %s", n, check.what)
		}
	}
}

// ---------------------------------------------------------------------------
// Item 1: the account's rows in Keel's job store

// rowsNaming is the store-wide read the first clause is answered with: every
// TEXT and BLOB column of every table, searched for one token. It is the
// question "does any row anywhere still name this", asked of the whole file
// rather than of the tables the deletion code happens to mention.
func rowsNaming(t *testing.T, db *sqlite.DB, token string) []string {
	t.Helper()
	if token == "" {
		t.Fatal("an empty token names everything")
	}
	var out []string
	for _, table := range schemaTables(t, db) {
		key, cols := textColumns(t, db, table)
		if len(cols) == 0 {
			continue
		}
		parts := make([]string, 0, len(cols))
		args := make([]any, 0, len(cols))
		for _, column := range cols {
			parts = append(parts, `instr("`+column+`", ?) > 0`)
			args = append(args, token)
		}
		query := `SELECT "` + key + `" FROM "` + table + `" WHERE ` + strings.Join(parts, " OR ")
		rows, err := db.Reader().QueryContext(t.Context(), query, args...)
		if err != nil {
			t.Fatalf("search %s for %s: %v", table, token, err)
		}
		for rows.Next() {
			var found string
			if err := rows.Scan(&found); err != nil {
				rows.Close()
				t.Fatalf("search %s: %v", table, err)
			}
			out = append(out, table+"="+found)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("search %s: %v", table, err)
		}
		rows.Close()
	}
	return out
}

// schemaTables lists the tables of one file, alphabetical, without SQLite's
// own bookkeeping.
func schemaTables(t *testing.T, db *sqlite.DB) []string {
	t.Helper()
	rows, err := db.Reader().QueryContext(t.Context(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("the table list: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("the table list: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("the table list: %v", err)
	}
	return out
}

// textColumns answers a table's first column, which is what a hit is named by,
// and its TEXT and BLOB columns, which are the only ones that can carry an id
// or a sentence.
func textColumns(t *testing.T, db *sqlite.DB, table string) (string, []string) {
	t.Helper()
	rows, err := db.Reader().QueryContext(t.Context(),
		`SELECT name, type FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatalf("the columns of %s: %v", table, err)
	}
	defer rows.Close()
	var first string
	var cols []string
	for rows.Next() {
		var name, decl string
		if err := rows.Scan(&name, &decl); err != nil {
			t.Fatalf("the columns of %s: %v", table, err)
		}
		if first == "" {
			first = name
		}
		switch strings.ToUpper(decl) {
		case "TEXT", "BLOB", "":
			cols = append(cols, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("the columns of %s: %v", table, err)
	}
	if first == "" {
		t.Fatalf("%s has no columns", table)
	}
	return first, cols
}

func bytesEqual(a, b []byte) bool { return bytes.Equal(a, b) }

// realBlobs is the surface privacy.Blobs asks of the mediastore, filled by the
// production store and its index: the index lists the blobs, and the store's
// Delete takes the row and then the bytes. An id that never had a row is
// absent from the list, which is what a minted id of a failed persist is.
type realBlobs struct {
	media *mediastore.Store
	index mediastore.BlobIndex
}

func (r realBlobs) Blobs(ctx context.Context) ([]mediastore.Blob, error) {
	return r.index.Blobs(ctx)
}

func (r realBlobs) Delete(ctx context.Context, mediaID string) error {
	return r.media.Delete(ctx, mediaID)
}
