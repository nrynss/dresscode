package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nrynss/dresscode/internal/domain"
	"github.com/nrynss/keel/fetch"
	"github.com/nrynss/keel/flag"
	flagsql "github.com/nrynss/keel/flag/sqlitestore"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/identity"
	"github.com/nrynss/keel/mediastore"
	mediasql "github.com/nrynss/keel/mediastore/sqlitestore"
	"github.com/nrynss/keel/photo"
	"github.com/nrynss/keel/upload"
	"github.com/nrynss/keel/wire"
)

const (
	routeUploads      = "/api/uploads"
	routePhotos       = "POST /api/photos"
	routeProducts     = "POST /api/products"
	routeProductLink  = "POST /api/products/link"
	routeMedia        = "GET /media/{id}"
	groupSelfie       = "selfie"
	groupProduct      = "product"
	youCamLongSide    = 1024
	clefMaxBytes      = 4 << 20
	minJudgeSide      = 64
	maxPhotoInput     = 64 << 20
	maxOwnerBytes     = 512 << 20
	maxLabelRunes     = 120
	maxCategoryRunes  = 40
	maxLinkRunes      = 2048
	maxJSONBody       = 64 << 10
	msgInvalid        = "The request is not valid."
	msgNotFound       = "No such resource exists."
	msgInternal       = "The request could not be finished."
	msgUploadsPaused  = "uploads paused"
	msgSamplesOnly    = "Only samples are open."
	msgUploadAPhoto   = "Upload a photo instead."
	msgTooSmall       = "The photo is too small."
	codeUploadsPaused = "uploads_paused"
	codeSamplesOnly   = "samples_only"
	codeUploadAPhoto  = "upload_a_photo"
	codeTooSmall      = "too_small"
	codeInvalid       = "invalid_request"
)

// freshUploadsOffFlag refuses a selfie open and POST /api/photos.
// samplesOnlyFlag refuses every fresh upload, photo, product, and link.
// Both are read on the request, so a flip is visible without a restart.
var (
	freshUploadsOffFlag = flag.Bool{
		Name:    "fresh_uploads_off",
		Default: false,
		Help:    "Refuse a fresh selfie upload and POST /api/photos.",
	}
	samplesOnlyFlag = flag.Bool{
		Name:    "samples_only",
		Default: false,
		Help:    "Refuse fresh uploads, photos, products, and links.",
	}
)

var (
	errTooSmall  = errors.New("intake: photo is too small")
	errNoPreview = errors.New("intake: no product image")
	errBadID     = errors.New("intake: bad id")
	errWrongKind = errors.New("intake: wrong upload kind")
)

// intakes remembers the stores mounted for each server. Close drops the entry.
var intakes sync.Map

type intake struct {
	media   *mediastore.Store
	uploads *upload.Handler
	flags   flag.Store
	log     *slog.Logger

	fetchMu  sync.RWMutex
	fetchCfg fetch.Config
	fetchSet bool
}

func (in *intake) close() {
	if in == nil || in.uploads == nil {
		return
	}
	if err := in.uploads.Close(); err != nil {
		in.log.Error("server: intake close", "err", err.Error())
	}
}

func (s *Server) intakeOf() *intake {
	v, ok := intakes.Load(s)
	if !ok {
		return nil
	}
	return v.(*intake)
}

func (s *Server) closeIntake() {
	v, ok := intakes.LoadAndDelete(s)
	if !ok {
		return
	}
	v.(*intake).close()
}

// mountIntake serves resumable uploads into mediastore, normalises every
// photo, and accepts a product link. A share target has no route of its
// own: the installed app reads the share on the device and calls the link
// path or the upload path. The camera uses the same upload path.
func (s *Server) mountIntake(ctx context.Context, cfg Config) error {
	in, err := openIntake(ctx, s, cfg)
	if err != nil {
		return fmt.Errorf("server: open: %w", err)
	}
	s.mux.Handle(routeUploads, http.HandlerFunc(s.serveUpload))
	s.mux.Handle(routeUploads+"/", http.HandlerFunc(s.serveUpload))
	s.mux.Handle(routePhotos, http.HandlerFunc(s.servePhoto))
	s.mux.Handle(routeProducts, http.HandlerFunc(s.serveProduct))
	s.mux.Handle(routeProductLink, http.HandlerFunc(s.serveProductLink))
	s.mux.Handle(routeMedia, in.media)
	intakes.Store(s, in)
	in.uploads.Start()
	return nil
}

func openIntake(ctx context.Context, s *Server, cfg Config) (*intake, error) {
	root := filepath.Dir(cfg.DBPath)
	mediaDir := filepath.Join(root, "media")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return nil, fmt.Errorf("media dir: %w", err)
	}
	index, err := mediasql.Open(ctx, mediasql.Config{DB: s.db})
	if err != nil {
		return nil, err
	}
	flags, err := flagsql.Open(ctx, flagsql.Config{DB: s.db, Now: cfg.Now, Logger: s.log})
	if err != nil {
		return nil, err
	}
	media, err := mediastore.Open(ctx, mediastore.Config{
		Dir:   mediaDir,
		Index: index,
		Log:   s.log,
		Now:   cfg.Now,
		ContentTypes: []string{
			"image/jpeg",
			"image/png",
			"image/webp",
			"image/heic",
			"image/heif",
		},
		Authorize: authorizeOwner,
	})
	if err != nil {
		return nil, err
	}
	uploads, err := upload.New(upload.Config{
		Dir:            filepath.Join(root, "upload-staging"),
		Store:          media,
		BasePath:       routeUploads,
		MaxUploadBytes: maxPhotoInput,
		MaxOwnerBytes:  maxOwnerBytes,
		Now:            cfg.Now,
		Log:            s.log,
	})
	if err != nil {
		return nil, err
	}
	return &intake{media: media, uploads: uploads, flags: flags, log: s.log}, nil
}

func authorizeOwner(r *http.Request, b mediastore.Blob) bool {
	user, ok := identity.UserFromContext(r.Context())
	return ok && user.ID != "" && user.ID == b.Owner
}

func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request) {
	in := s.intakeOf()
	if in == nil {
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if r.Method == http.MethodPost && path.Clean("/"+r.URL.Path) == routeUploads {
		s.beginUpload(w, r, in)
		return
	}
	in.uploads.ServeHTTP(w, r)
}

// beginUpload rewrites the open before Keel sees it. Keel stores the
// owner and the visibility from the body, and this path must ignore both:
// the owner is the session user, and the blob is private.
func (s *Server) beginUpload(w http.ResponseWriter, r *http.Request, in *intake) {
	if !s.haveUser(w, r) {
		return
	}
	if s.refuseFlag(w, r, samplesOnlyFlag, http.StatusForbidden, codeSamplesOnly, msgSamplesOnly) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBody+1))
	if err != nil {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "body", Reason: "must be a JSON object"})
		return
	}
	if len(body) > maxJSONBody {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "body", Reason: "is too large"})
		return
	}
	var req uploadBegin
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "body", Reason: "must be a JSON object"})
		return
	}
	group := strings.TrimSpace(req.Group)
	if group == "" {
		group = groupSelfie
	}
	if group != groupSelfie && group != groupProduct {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "group", Reason: "must be selfie or product"})
		return
	}
	if group == groupSelfie && s.refuseFlag(w, r, freshUploadsOffFlag, http.StatusForbidden, codeUploadsPaused, msgUploadsPaused) {
		return
	}
	user, _ := identity.UserFromContext(r.Context())
	req.Owner = user.ID
	req.Visibility = "private"
	req.Group = group
	rewritten, err := json.Marshal(req)
	if err != nil {
		s.log.Error("server: intake rewrite", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	next := r.Clone(r.Context())
	next.Body = io.NopCloser(bytes.NewReader(rewritten))
	next.ContentLength = int64(len(rewritten))
	in.uploads.ServeHTTP(w, next)
}

type uploadBegin struct {
	Owner       string `json:"owner"`
	ContentType string `json:"content_type"`
	Group       string `json:"group"`
	Visibility  string `json:"visibility"`
	SizeBytes   *int64 `json:"size_bytes,omitempty"`
	ChunkSize   int64  `json:"chunk_size,omitempty"`
}

func (s *Server) servePhoto(w http.ResponseWriter, r *http.Request) {
	if !s.haveUser(w, r) {
		return
	}
	if s.refuseFlag(w, r, samplesOnlyFlag, http.StatusForbidden, codeSamplesOnly, msgSamplesOnly) {
		return
	}
	if s.refuseFlag(w, r, freshUploadsOffFlag, http.StatusForbidden, codeUploadsPaused, msgUploadsPaused) {
		return
	}
	fields, ok := s.readObject(w, r, "upload_id")
	if !ok {
		return
	}
	uploadID, ok := s.requiredID(w, fields, "upload_id")
	if !ok {
		return
	}
	user, _ := identity.UserFromContext(r.Context())
	raw, err := s.rawUpload(r.Context(), user.ID, uploadID, groupSelfie)
	if err != nil {
		s.writeRawError(w, err, "is not a selfie upload")
		return
	}
	stored, err := s.normalise(r.Context(), user.ID, groupSelfie, raw)
	if err != nil {
		// The raw blob is the only copy of the GPS. A refusal must not leave it.
		s.discardRaw(r.Context(), uploadID)
		s.writePhotoError(w, err)
		return
	}
	if err := s.dropRaw(r.Context(), uploadID, stored.id); err != nil {
		s.log.Error("server: intake drop raw", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if err := writeJSON(w, http.StatusCreated, photoBody{
		MediaID:     stored.id,
		URL:         mediaURL(stored.id),
		ContentType: "image/jpeg",
		SHA256:      stored.sha,
	}); err != nil {
		s.log.Warn("server: intake photo", "err", err.Error())
	}
}

func (s *Server) serveProduct(w http.ResponseWriter, r *http.Request) {
	if !s.haveUser(w, r) {
		return
	}
	if s.refuseFlag(w, r, samplesOnlyFlag, http.StatusForbidden, codeSamplesOnly, msgSamplesOnly) {
		return
	}
	fields, ok := s.readObject(w, r, "upload_id", "name", "category")
	if !ok {
		return
	}
	uploadID, ok := s.requiredID(w, fields, "upload_id")
	if !ok {
		return
	}
	name, ok := s.requiredLabel(w, fields, "name", maxLabelRunes)
	if !ok {
		return
	}
	category, ok := s.requiredLabel(w, fields, "category", maxCategoryRunes)
	if !ok {
		return
	}
	user, _ := identity.UserFromContext(r.Context())
	raw, err := s.rawUpload(r.Context(), user.ID, uploadID, groupProduct)
	if err != nil {
		// Chaaya's open body has no group field - its beginBody sends owner,
		// content_type, visibility and chunk_size only - so an app upload cannot
		// open as a product, and every client upload lands in the selfie group.
		// The owner match above still gates this path, and the bytes are
		// immutable and unread-only here, so the second open costs nothing. The
		// issue text for Chaaya is in the diary; when the uploader grows a group
		// field, this fallback goes.
		raw, err = s.rawUpload(r.Context(), user.ID, uploadID, groupSelfie)
	}
	if err != nil {
		s.writeRawError(w, err, "is not a product upload")
		return
	}
	s.finishProduct(w, r, user.ID, name, category, domain.ExactProduct, raw, uploadID)
}

func (s *Server) serveProductLink(w http.ResponseWriter, r *http.Request) {
	if !s.haveUser(w, r) {
		return
	}
	if s.refuseFlag(w, r, samplesOnlyFlag, http.StatusForbidden, codeSamplesOnly, msgSamplesOnly) {
		return
	}
	fields, ok := s.readObject(w, r, "url")
	if !ok {
		return
	}
	rawURL, ok := s.requiredText(w, fields, "url")
	if !ok {
		return
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "url", Reason: "must not be empty"})
		return
	}
	if len([]rune(rawURL)) > maxLinkRunes {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "url", Reason: "is too long"})
		return
	}
	title, category, image, pageURL, err := s.fetchPreview(r.Context(), rawURL)
	if err != nil {
		if errors.Is(err, errNoPreview) {
			writeAPI(w, http.StatusUnprocessableEntity, codeUploadAPhoto, msgUploadAPhoto, nil)
			return
		}
		if s.writeFetch(w, err) {
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		s.log.Error("server: intake link", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	name := cleanLabel(title, maxLabelRunes)
	if name == "" {
		name = nameFromURL(pageURL)
	}
	if name == "" {
		writeAPI(w, http.StatusUnprocessableEntity, codeUploadAPhoto, msgUploadAPhoto, nil)
		return
	}
	category = cleanLabel(category, maxCategoryRunes)
	user, _ := identity.UserFromContext(r.Context())
	// A preview that will not normalise is not a usable image. The
	// private-address refusal is written above and is not this code.
	stored, err := s.normalise(r.Context(), user.ID, groupProduct, image)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		var pe *photo.Error
		if errors.Is(err, errTooSmall) || errors.As(err, &pe) {
			writeAPI(w, http.StatusUnprocessableEntity, codeUploadAPhoto, msgUploadAPhoto, nil)
			return
		}
		s.log.Error("server: intake link image", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if err := s.insertProduct(r.Context(), stored.id, user.ID, category, name, domain.ApproximateMatch); err != nil {
		_ = s.intakeOf().media.Delete(r.Context(), stored.id)
		s.log.Error("server: intake product", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if err := writeJSON(w, http.StatusCreated, newProductBody(stored.id, category, name, domain.ApproximateMatch)); err != nil {
		s.log.Warn("server: intake link", "err", err.Error())
	}
}

func (s *Server) finishProduct(w http.ResponseWriter, r *http.Request, userID, name, category string, fidelity domain.Fidelity, raw []byte, uploadID string) {
	stored, err := s.normalise(r.Context(), userID, groupProduct, raw)
	if err != nil {
		s.discardRaw(r.Context(), uploadID)
		s.writePhotoError(w, err)
		return
	}
	if err := s.insertProduct(r.Context(), stored.id, userID, category, name, fidelity); err != nil {
		_ = s.intakeOf().media.Delete(r.Context(), stored.id)
		s.log.Error("server: intake product", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if err := s.dropRaw(r.Context(), uploadID, stored.id); err != nil {
		_ = s.deleteProduct(r.Context(), stored.id)
		s.log.Error("server: intake drop raw", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return
	}
	if err := writeJSON(w, http.StatusCreated, newProductBody(stored.id, category, name, fidelity)); err != nil {
		s.log.Warn("server: intake product", "err", err.Error())
	}
}

type storedPhoto struct {
	id  string
	sha string
}

func (s *Server) normalise(ctx context.Context, owner, group string, raw []byte) (storedPhoto, error) {
	res, err := photo.Normalize(ctx, bytes.NewReader(raw), photo.Config{
		MaxLongSide:   youCamLongSide,
		MaxBytes:      clefMaxBytes,
		MaxInputBytes: maxPhotoInput,
	})
	if err != nil {
		return storedPhoto{}, err
	}
	// The long side is the bound. A photo under it cannot be judged.
	// There is no face detector here: a cached miss is the only way this
	// path could already know, and no such row is written at intake.
	if res.Width < minJudgeSide && res.Height < minJudgeSide {
		return storedPhoto{}, errTooSmall
	}
	mediaID, err := s.intakeOf().media.Persist(ctx, bytes.NewReader(res.Bytes), mediastore.Put{
		ContentType: "image/jpeg",
		Owner:       owner,
		Group:       group,
		Visibility:  mediastore.Private,
	})
	if err != nil {
		return storedPhoto{}, err
	}
	return storedPhoto{id: mediaID, sha: res.SHA256}, nil
}

func (s *Server) rawUpload(ctx context.Context, userID, uploadID, group string) ([]byte, error) {
	if !id.Valid(uploadID) {
		return nil, errBadID
	}
	rc, blob, err := s.intakeOf().media.Open(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	if blob.Owner != userID {
		return nil, mediastore.ErrNotFound
	}
	if blob.Group != group {
		return nil, errWrongKind
	}
	raw, err := io.ReadAll(io.LimitReader(rc, maxPhotoInput+1))
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (s *Server) discardRaw(ctx context.Context, uploadID string) {
	if err := s.intakeOf().media.Delete(ctx, uploadID); err != nil && !errors.Is(err, mediastore.ErrNotFound) {
		s.log.Error("server: intake drop raw", "err", err.Error())
	}
}

func (s *Server) dropRaw(ctx context.Context, uploadID, keptID string) error {
	err := s.intakeOf().media.Delete(ctx, uploadID)
	if err == nil || errors.Is(err, mediastore.ErrNotFound) {
		return nil
	}
	if keptID != "" {
		_ = s.intakeOf().media.Delete(ctx, keptID)
	}
	return err
}

func (s *Server) insertProduct(ctx context.Context, id, userID, category, name string, fidelity domain.Fidelity) error {
	_, err := s.db.Writer().ExecContext(ctx,
		`INSERT INTO products (id, user_id, category, name, fidelity, origin, youcam_role, photograph)
		 VALUES (?, ?, ?, ?, ?, ?, '', '')`,
		id, userID, category, name, string(fidelity), string(domain.OriginUser))
	if err != nil {
		return fmt.Errorf("insert product: %w", err)
	}
	return nil
}

func (s *Server) deleteProduct(ctx context.Context, id string) error {
	_, err := s.db.Writer().ExecContext(ctx, `DELETE FROM products WHERE id = ?`, id)
	return err
}

func (s *Server) fetchPreview(ctx context.Context, rawURL string) (title, category string, image []byte, pageURL string, err error) {
	cfg := s.intakeOf().pageFetch()
	resp, err := fetch.Get(ctx, rawURL, cfg)
	if err != nil {
		return "", "", nil, "", err
	}
	kind := bareMediaType(resp.Header.Get("Content-Type"))
	switch kind {
	case "image/jpeg", "image/png", "image/webp":
		return "", "", resp.Body, resp.URL, nil
	case "text/html", "application/xhtml+xml":
		title, imageURL := fetch.Meta(resp)
		category = metaContent(resp.Body, "product:category")
		if category == "" {
			category = metaContent(resp.Body, "og:category")
		}
		if imageURL == "" {
			return "", "", nil, resp.URL, errNoPreview
		}
		imgCfg := cfg
		imgCfg.ContentTypes = []string{"image/jpeg", "image/png", "image/webp"}
		img, err := fetch.Get(ctx, imageURL, imgCfg)
		if err != nil {
			return "", "", nil, resp.URL, err
		}
		return title, category, img.Body, resp.URL, nil
	default:
		return "", "", nil, resp.URL, errNoPreview
	}
}

func (in *intake) pageFetch() fetch.Config {
	in.fetchMu.RLock()
	defer in.fetchMu.RUnlock()
	if in.fetchSet {
		return in.fetchCfg
	}
	return fetch.Config{
		Timeout:  15 * time.Second,
		MaxBytes: maxPhotoInput,
		ContentTypes: []string{
			"text/html",
			"application/xhtml+xml",
			"image/jpeg",
			"image/png",
			"image/webp",
		},
		Log: in.log,
	}
}

// setLinkFetch replaces the product-link policy. Production leaves it
// unset, which refuses a private address. A test points it at loopback.
func (s *Server) setLinkFetch(cfg fetch.Config) {
	in := s.intakeOf()
	in.fetchMu.Lock()
	in.fetchCfg = cfg
	in.fetchSet = true
	in.fetchMu.Unlock()
}

func (s *Server) setIntakeFlag(ctx context.Context, f flag.Bool, value bool) error {
	_, err := s.intakeOf().flags.SetBool(ctx, f, value)
	return err
}

func (s *Server) haveUser(w http.ResponseWriter, r *http.Request) bool {
	user, ok := identity.UserFromContext(r.Context())
	if !ok || !id.Valid(user.ID) {
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return false
	}
	return true
}

func (s *Server) refuseFlag(w http.ResponseWriter, r *http.Request, f flag.Bool, status int, code, message string) bool {
	on, _, err := s.intakeOf().flags.Bool(r.Context(), f)
	if err != nil {
		s.log.Error("server: intake flag", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
		return true
	}
	if !on {
		return false
	}
	writeAPI(w, status, code, message, nil)
	return true
}

func (s *Server) writeRawError(w http.ResponseWriter, err error, kindReason string) {
	switch {
	case errors.Is(err, errBadID):
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "upload_id", Reason: "must be an id"})
	case errors.Is(err, errWrongKind):
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "upload_id", Reason: kindReason})
	case errors.Is(err, mediastore.ErrNotFound):
		writeAPI(w, http.StatusNotFound, wire.CodeNotFound, msgNotFound, nil)
	default:
		s.log.Error("server: intake upload", "err", err.Error())
		writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
	}
}

func (s *Server) writePhotoError(w http.ResponseWriter, err error) {
	if errors.Is(err, errTooSmall) {
		writeAPI(w, http.StatusUnprocessableEntity, codeTooSmall, msgTooSmall, nil)
		return
	}
	var pe *photo.Error
	if errors.As(err, &pe) && pe.Code() != "" {
		if msg, ok := photoMessage[pe.Code()]; ok {
			writeAPI(w, http.StatusUnprocessableEntity, pe.Code(), msg, nil)
			return
		}
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	s.log.Error("server: intake photo", "err", err.Error())
	writeAPI(w, http.StatusInternalServerError, wire.CodeInternal, msgInternal, nil)
}

func (s *Server) writeFetch(w http.ResponseWriter, err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	var fe *fetch.Error
	if !errors.As(err, &fe) {
		return false
	}
	msg, ok := fetchMessage[fe.Code]
	if !ok {
		return false
	}
	writeAPI(w, http.StatusUnprocessableEntity, fe.Code, msg, nil)
	return true
}

var photoMessage = map[string]string{
	"unsupported_format":   "The photo could not be read.",
	"heic":                 "This photo is HEIC, which this app does not convert.",
	"too_many_pixels":      "The photo has too many pixels.",
	"too_many_input_bytes": "The photo is too large.",
	"output_cannot_fit":    "The photo could not be stored small enough.",
}

var fetchMessage = map[string]string{
	fetch.CodeBlockedAddress:   "That link was refused.",
	fetch.CodeBadScheme:        "The link must be https.",
	fetch.CodeBadURL:           "The link could not be read.",
	fetch.CodeBadPort:          "The link was refused.",
	fetch.CodeTooManyRedirects: "The link redirected too many times.",
	fetch.CodeTooLarge:         "The link was too large.",
	fetch.CodeBadType:          "The link was not a page or an image this app can use.",
	fetch.CodeTimeout:          "The link took too long.",
	fetch.CodeBadStatus:        "The link did not answer.",
	fetch.CodeUnreachable:      "The link could not be reached.",
}

type fieldDetail struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

type photoBody struct {
	MediaID     string `json:"media_id"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	SHA256      string `json:"sha256"`
}

type productBody struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Name     string `json:"name"`
	Fidelity string `json:"fidelity"`
	Origin   string `json:"origin"`
	MediaID  string `json:"media_id,omitempty"`
	URL      string `json:"url,omitempty"`
}

func newProductBody(id, category, name string, fidelity domain.Fidelity) productBody {
	return productBody{
		ID:       id,
		Category: category,
		Name:     name,
		Fidelity: string(fidelity),
		Origin:   string(domain.OriginUser),
		MediaID:  id,
		URL:      mediaURL(id),
	}
}

func mediaURL(id string) string {
	return "/media/" + id
}

func writeAPI(w http.ResponseWriter, status int, code, message string, detail any) {
	_ = wire.WriteError(w, status, code, message, detail)
}

func (s *Server) readObject(w http.ResponseWriter, r *http.Request, allow ...string) (map[string]json.RawMessage, bool) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxJSONBody+1))
	var fields map[string]json.RawMessage
	if err := dec.Decode(&fields); err != nil || fields == nil {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "body", Reason: "must be a JSON object"})
		return nil, false
	}
	if dec.More() {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: "body", Reason: "must be one JSON object"})
		return nil, false
	}
	allowed := make(map[string]bool, len(allow))
	for _, name := range allow {
		allowed[name] = true
	}
	for key := range fields {
		if !allowed[key] {
			writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: key, Reason: "is not a field on this path"})
			return nil, false
		}
	}
	return fields, true
}

func (s *Server) requiredText(w http.ResponseWriter, fields map[string]json.RawMessage, name string) (string, bool) {
	raw, ok := fields[name]
	if !ok {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: name, Reason: "is required"})
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: name, Reason: "must be text"})
		return "", false
	}
	return text, true
}

func (s *Server) requiredID(w http.ResponseWriter, fields map[string]json.RawMessage, name string) (string, bool) {
	text, ok := s.requiredText(w, fields, name)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if !id.Valid(text) {
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: name, Reason: "must be an id"})
		return "", false
	}
	return text, true
}

func (s *Server) requiredLabel(w http.ResponseWriter, fields map[string]json.RawMessage, name string, max int) (string, bool) {
	text, ok := s.requiredText(w, fields, name)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: name, Reason: "must not be empty"})
		return "", false
	case strings.ContainsRune(text, 0) || strings.ContainsAny(text, "\r\n"):
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: name, Reason: "must be one line"})
		return "", false
	case len([]rune(text)) > max:
		writeAPI(w, http.StatusBadRequest, codeInvalid, msgInvalid, fieldDetail{Field: name, Reason: "is too long"})
		return "", false
	}
	return text, true
}

func cleanLabel(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		case 0:
			return -1
		default:
			return r
		}
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > max {
		s = strings.TrimSpace(string(runes[:max]))
	}
	return s
}

func nameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	base := path.Base(u.Path)
	base = strings.TrimSuffix(base, path.Ext(base))
	base = strings.NewReplacer("-", " ", "_", " ").Replace(base)
	return cleanLabel(base, maxLabelRunes)
}

func bareMediaType(value string) string {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return strings.ToLower(mediaType)
}

func metaContent(html []byte, key string) string {
	lower := strings.ToLower(string(html))
	key = strings.ToLower(key)
	for _, attr := range []string{"property", "name"} {
		for _, quote := range []string{`"`, `'`} {
			needle := attr + "=" + quote + key + quote
			start := 0
			for {
				rel := strings.Index(lower[start:], needle)
				if rel < 0 {
					break
				}
				i := start + rel
				tagStart := strings.LastIndex(lower[:i], "<")
				relEnd := strings.Index(lower[i:], ">")
				if tagStart >= 0 && relEnd >= 0 {
					tag := string(html[tagStart : i+relEnd])
					if v := htmlAttr(tag, "content"); v != "" {
						return strings.TrimSpace(v)
					}
				}
				start = i + len(needle)
			}
		}
	}
	return ""
}

func htmlAttr(tag, name string) string {
	lower := strings.ToLower(tag)
	for _, quote := range []byte{'"', '\''} {
		needle := strings.ToLower(name) + "=" + string(quote)
		i := strings.Index(lower, needle)
		if i < 0 {
			continue
		}
		rest := tag[i+len(needle):]
		end := strings.IndexByte(rest, quote)
		if end < 0 {
			continue
		}
		return rest[:end]
	}
	return ""
}
