package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nrynss/keel/fetch"
	"github.com/nrynss/keel/id"
)

const appIndex = "<!doctype html><title>Dresscode</title>"

func TestPhotoWithGPSComesOutWithoutIt(t *testing.T) {
	t.Parallel()
	srv := openIntakeServer(t)
	h := srv.Handler()
	userID, cookie := bootUser(t, h)

	marker := "GPS 51.5000 -0.1200"
	body := jpegWithEXIF(t, 80, 40, 6, marker)
	if !bytes.Contains(body, []byte(marker)) {
		t.Fatal("fixture lost the GPS text")
	}
	uploadID := uploadBytes(t, h, cookie, userID, groupSelfie, "image/jpeg", body)
	photo := postPhoto(t, h, cookie, uploadID)

	if photo.ContentType != "image/jpeg" || !id.Valid(photo.MediaID) || photo.URL != "/media/"+photo.MediaID {
		t.Fatalf("photo response %+v", photo)
	}
	media := do(t, h, http.MethodGet, photo.URL, cookie, nil, nil)
	if media.Code != http.StatusOK {
		t.Fatalf("media %d %s", media.Code, media.Body.Bytes())
	}
	if media.Header().Get("Content-Type") != "image/jpeg" || media.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("media headers %v", media.Header())
	}
	out := media.Body.Bytes()
	if bytes.Contains(out, []byte(marker)) || bytes.Contains(out, []byte("Exif\x00\x00")) || bytes.Contains(out, []byte("GPS")) {
		t.Fatal("normalised photo still carries GPS or EXIF")
	}
	sum := sha256.Sum256(out)
	if hex.EncodeToString(sum[:]) != photo.SHA256 {
		t.Fatalf("body hash %x, response %s", sum, photo.SHA256)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 40 || cfg.Height != 80 {
		t.Fatalf("oriented size %dx%d, want 40x80", cfg.Width, cfg.Height)
	}

	other := jpegWithEXIF(t, 80, 40, 6, "GPS 48.8600 2.3400 other note")
	otherID := uploadBytes(t, h, cookie, userID, groupSelfie, "image/jpeg", other)
	otherPhoto := postPhoto(t, h, cookie, otherID)
	if otherPhoto.SHA256 != photo.SHA256 {
		t.Fatalf("same pixels hashed %s and %s", photo.SHA256, otherPhoto.SHA256)
	}

	raw := do(t, h, http.MethodGet, "/media/"+uploadID, cookie, nil, nil)
	if raw.Code != http.StatusNotFound || bytes.Contains(raw.Body.Bytes(), []byte(marker)) {
		t.Fatalf("raw upload still readable: %d %q", raw.Code, raw.Body.Bytes())
	}
	anon := do(t, h, http.MethodGet, photo.URL, nil, nil, nil)
	if anon.Code != http.StatusNotFound || bytes.Contains(anon.Body.Bytes(), []byte("GPS")) {
		t.Fatal("private photo was served without its owner")
	}

	wide := jpegWithEXIF(t, 1600, 800, 1, "")
	wideID := uploadBytes(t, h, cookie, userID, groupSelfie, "image/jpeg", wide)
	widePhoto := postPhoto(t, h, cookie, wideID)
	wideMedia := do(t, h, http.MethodGet, widePhoto.URL, cookie, nil, nil)
	wideOut := wideMedia.Body.Bytes()
	wideCfg, err := jpeg.DecodeConfig(bytes.NewReader(wideOut))
	if err != nil {
		t.Fatal(err)
	}
	if wideCfg.Width != 1024 || wideCfg.Height != 512 {
		t.Fatalf("resized %dx%d, want 1024x512", wideCfg.Width, wideCfg.Height)
	}
	if len(wideOut) > clefMaxBytes {
		t.Fatalf("output %d bytes, over Clef's cap", len(wideOut))
	}
}

func TestPrivateAddressIsRefused(t *testing.T) {
	t.Parallel()
	srv := openIntakeServer(t)
	h := srv.Handler()
	_, cookie := bootUser(t, h)
	for _, rawURL := range []string{
		"https://10.1.2.3/hidden",
		"https://192.168.9.4/item",
		"https://127.0.0.1/local",
	} {
		t.Run(rawURL, func(t *testing.T) {
			rec := postJSON(t, h, "/api/products/link", cookie, map[string]string{"url": rawURL})
			assertErr(t, rec, http.StatusUnprocessableEntity, "fetch_blocked_address", "That link was refused.")
			if strings.Contains(rec.Body.String(), "10.") || strings.Contains(rec.Body.String(), "192.168") || strings.Contains(rec.Body.String(), "127.0.0.1") || strings.Contains(rec.Body.String(), "hidden") {
				t.Fatalf("refusal echoes the address: %s", rec.Body.Bytes())
			}
		})
	}
	scheme := postJSON(t, h, "/api/products/link", cookie, map[string]string{"url": "http://10.1.2.3/hidden"})
	assertErr(t, scheme, http.StatusUnprocessableEntity, "fetch_bad_scheme", "The link must be https.")
}

func TestBlockedFetchReturnsUploadAPhoto(t *testing.T) {
	t.Parallel()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/blocked":
			fmt.Fprint(w, `<!doctype html><title>Access denied</title><p>This retailer blocked the fetch.</p>`)
		case "/gone":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(page.Close)
	srv := openIntakeServer(t)
	srv.setLinkFetch(loopbackFetch(t, page.URL))
	h := srv.Handler()
	_, cookie := bootUser(t, h)

	blocked := postJSON(t, h, "/api/products/link", cookie, map[string]string{"url": page.URL + "/blocked"})
	assertErr(t, blocked, http.StatusUnprocessableEntity, "upload_a_photo", "Upload a photo instead.")

	gone := postJSON(t, h, "/api/products/link", cookie, map[string]string{"url": page.URL + "/gone"})
	assertErr(t, gone, http.StatusUnprocessableEntity, "fetch_bad_status", "The link did not answer.")
}

func TestProductLinkStoresThePreview(t *testing.T) {
	t.Parallel()
	marker := "GPS 12.3400 56.7800"
	preview := jpegWithEXIF(t, 96, 64, 1, marker)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gold-jhumkas":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><meta property="og:title" content="Gold jhumkas">`)
			fmt.Fprint(w, `<meta property="product:category" content="earrings">`)
			fmt.Fprint(w, `<meta property="og:image" content="/j.jpg">`)
		case "/j.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(preview)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(page.Close)
	srv := openIntakeServer(t)
	srv.setLinkFetch(loopbackFetch(t, page.URL))
	h := srv.Handler()
	userID, cookie := bootUser(t, h)

	rec := postJSON(t, h, "/api/products/link", cookie, map[string]string{"url": page.URL + "/gold-jhumkas"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("link %d %s", rec.Code, rec.Body.Bytes())
	}
	var got productBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Gold jhumkas" || got.Category != "earrings" || got.Fidelity != "approximate match" || got.Origin != "user" || got.ID != got.MediaID {
		t.Fatalf("product %+v", got)
	}
	media := do(t, h, http.MethodGet, got.URL, cookie, nil, nil)
	if media.Code != http.StatusOK || bytes.Contains(media.Body.Bytes(), []byte(marker)) || bytes.Contains(media.Body.Bytes(), []byte("Exif\x00\x00")) {
		t.Fatalf("preview kept GPS or failed: %d", media.Code)
	}
	var category, name, fidelity, origin, owner string
	if err := srv.db.Reader().QueryRowContext(t.Context(),
		`SELECT category, name, fidelity, origin, user_id FROM products WHERE id = ?`, got.ID,
	).Scan(&category, &name, &fidelity, &origin, &owner); err != nil {
		t.Fatal(err)
	}
	if category != "earrings" || name != "Gold jhumkas" || fidelity != "approximate match" || origin != "user" || owner != userID {
		t.Fatalf("row %s %s %s %s %s", category, name, fidelity, origin, owner)
	}
}

func TestProductPhotoIsExact(t *testing.T) {
	t.Parallel()
	srv := openIntakeServer(t)
	h := srv.Handler()
	userID, cookie := bootUser(t, h)
	body := jpegWithEXIF(t, 70, 70, 1, "GPS 1 2")
	uploadID := uploadBytes(t, h, cookie, userID, groupProduct, "image/jpeg", body)
	rec := postJSON(t, h, "/api/products", cookie, map[string]string{
		"upload_id": uploadID,
		"name":      "Gold jhumkas",
		"category":  "earrings",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("product %d %s", rec.Code, rec.Body.Bytes())
	}
	var got productBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Fidelity != "exact product" || got.Origin != "user" || got.Name != "Gold jhumkas" || got.ID != got.MediaID {
		t.Fatalf("product %+v", got)
	}
	media := do(t, h, http.MethodGet, got.URL, cookie, nil, nil)
	if bytes.Contains(media.Body.Bytes(), []byte("GPS 1 2")) {
		t.Fatal("product photo kept GPS")
	}
}

func TestIntakeRefusals(t *testing.T) {
	t.Parallel()
	srv := openIntakeServer(t)
	h := srv.Handler()
	userID, cookie := bootUser(t, h)

	small := jpegWithEXIF(t, 16, 16, 1, "")
	smallID := uploadBytes(t, h, cookie, userID, groupSelfie, "image/jpeg", small)
	assertErr(t, postJSON(t, h, "/api/photos", cookie, map[string]string{"upload_id": smallID}), http.StatusUnprocessableEntity, "too_small", "The photo is too small.")

	badID := uploadBytes(t, h, cookie, userID, groupSelfie, "image/jpeg", []byte("not a photo"))
	assertErr(t, postJSON(t, h, "/api/photos", cookie, map[string]string{"upload_id": badID}), http.StatusUnprocessableEntity, "unsupported_format", "The photo could not be read.")

	heicID := uploadBytes(t, h, cookie, userID, groupSelfie, "image/heic", heicPrefix())
	assertErr(t, postJSON(t, h, "/api/photos", cookie, map[string]string{"upload_id": heicID}), http.StatusUnprocessableEntity, "heic", "This photo is HEIC, which this app does not convert.")

	extra := postJSON(t, h, "/api/photos", cookie, map[string]string{"upload_id": smallID, "note": "no"})
	assertErr(t, extra, http.StatusBadRequest, "invalid_request", "The request is not valid.")

	_, other := bootUser(t, h)
	hidden := postJSON(t, h, "/api/photos", other, map[string]string{"upload_id": smallID})
	assertErr(t, hidden, http.StatusNotFound, "not_found", "No such resource exists.")

	if err := srv.setIntakeFlag(t.Context(), freshUploadsOffFlag, true); err != nil {
		t.Fatal(err)
	}
	paused := postJSON(t, h, "/api/uploads", cookie, map[string]string{"content_type": "image/jpeg", "group": "selfie"})
	assertErr(t, paused, http.StatusForbidden, "uploads_paused", "uploads paused")
	productOpen := postJSON(t, h, "/api/uploads", cookie, map[string]string{"content_type": "image/jpeg", "group": "product"})
	if productOpen.Code != http.StatusCreated {
		t.Fatalf("product upload while selfies are paused: %d %s", productOpen.Code, productOpen.Body.Bytes())
	}
	assertErr(t, postJSON(t, h, "/api/photos", cookie, map[string]string{"upload_id": smallID}), http.StatusForbidden, "uploads_paused", "uploads paused")

	if err := srv.setIntakeFlag(t.Context(), samplesOnlyFlag, true); err != nil {
		t.Fatal(err)
	}
	both := postJSON(t, h, "/api/uploads", cookie, map[string]string{"content_type": "image/jpeg", "group": "selfie"})
	assertErr(t, both, http.StatusForbidden, "samples_only", "Only samples are open.")
	productPaused := postJSON(t, h, "/api/uploads", cookie, map[string]string{"content_type": "image/jpeg", "group": "product"})
	assertErr(t, productPaused, http.StatusForbidden, "samples_only", "Only samples are open.")
	link := postJSON(t, h, "/api/products/link", cookie, map[string]string{"url": "https://10.1.2.3/item"})
	assertErr(t, link, http.StatusForbidden, "samples_only", "Only samples are open.")
}

func TestOmittedUploadSizeAndGroup(t *testing.T) {
	t.Parallel()
	srv := openIntakeServer(t)
	h := srv.Handler()
	userID, cookie := bootUser(t, h)
	body := jpegWithEXIF(t, 72, 72, 1, "")
	rec := postJSON(t, h, "/api/uploads", cookie, map[string]string{
		"content_type": "image/jpeg",
		"visibility":   "public",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("begin %d %s", rec.Code, rec.Body.Bytes())
	}
	var begin struct {
		ID         string `json:"id"`
		Owner      string `json:"owner"`
		Visibility string `json:"visibility"`
		SizeBytes  *int64 `json:"size_bytes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &begin); err != nil {
		t.Fatal(err)
	}
	if begin.Owner != userID || begin.Visibility != "private" || begin.SizeBytes != nil {
		t.Fatalf("begin %+v", begin)
	}
	putChunk(t, h, cookie, begin.ID, 0, body)
	sum := sha256.Sum256(body)
	done := postJSON(t, h, "/api/uploads/"+begin.ID+"/complete", cookie, map[string]string{
		"sha256": hex.EncodeToString(sum[:]),
	})
	if done.Code != http.StatusCreated {
		t.Fatalf("complete %d %s", done.Code, done.Body.Bytes())
	}
	if photo := postPhoto(t, h, cookie, begin.ID); !id.Valid(photo.MediaID) {
		t.Fatalf("photo %+v", photo)
	}
}

func TestShareTargetHasNoAPIPath(t *testing.T) {
	t.Parallel()
	srv := openIntakeServer(t)
	h := srv.Handler()
	page := do(t, h, http.MethodGet, "/share?url=https://example.com/gold-jhumkas", nil, nil, nil)
	if page.Code != http.StatusOK || page.Body.String() != appIndex {
		t.Fatalf("share launch %d %q", page.Code, page.Body.String())
	}
	miss := do(t, h, http.MethodPost, "/api/share", nil, strings.NewReader(`{"url":"https://example.com"}`), nil)
	assertErr(t, miss, http.StatusNotFound, "not_found", "No such resource exists.")
}

func openIntakeServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "index.html"), []byte(appIndex), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := Open(t.Context(), Config{
		DBPath:     filepath.Join(dir, "dresscode.db"),
		AppDir:     app,
		SigningKey: []byte("test-session-signing-key-32b!!!!"),
		Log:        slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

func bootUser(t *testing.T, h http.Handler) (string, *http.Cookie) {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/session", nil, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("session %d %s", rec.Code, rec.Body.Bytes())
	}
	var body struct {
		UserID string `json:"user_id"`
		Kind   string `json:"kind"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Kind != "guest" || !id.Valid(body.UserID) {
		t.Fatalf("session %+v", body)
	}
	return body.UserID, sessionCookie(t, rec)
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "identity_session" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func do(t *testing.T, h http.Handler, method, path string, cookie *http.Cookie, body io.Reader, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.RemoteAddr = "192.0.2.1:1234"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postJSON(t *testing.T, h http.Handler, path string, cookie *http.Cookie, payload any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return do(t, h, http.MethodPost, path, cookie, bytes.NewReader(raw), nil)
}

func assertErr(t *testing.T, rec *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if !strings.HasSuffix(rec.Body.String(), "\n") {
		t.Fatalf("body has no newline: %q", rec.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != status || body.Error.Code != code || body.Error.Message != message {
		t.Fatalf("status %d code %q message %q, want %d %q %q", rec.Code, body.Error.Code, body.Error.Message, status, code, message)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("headers %v", rec.Header())
	}
}

type photoResponse struct {
	MediaID     string `json:"media_id"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	SHA256      string `json:"sha256"`
}

func postPhoto(t *testing.T, h http.Handler, cookie *http.Cookie, uploadID string) photoResponse {
	t.Helper()
	rec := postJSON(t, h, "/api/photos", cookie, map[string]string{"upload_id": uploadID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("photo %d %s", rec.Code, rec.Body.Bytes())
	}
	var got photoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func uploadBytes(t *testing.T, h http.Handler, cookie *http.Cookie, userID, group, contentType string, body []byte) string {
	t.Helper()
	chunkSize := (len(body) + 1) / 2
	if chunkSize < 1 {
		chunkSize = 1
	}
	rec := postJSON(t, h, "/api/uploads", cookie, map[string]any{
		"owner":        "ignored",
		"content_type": contentType,
		"visibility":   "public",
		"chunk_size":   chunkSize,
		"size_bytes":   len(body),
		"group":        group,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("begin %d %s", rec.Code, rec.Body.Bytes())
	}
	var begin struct {
		ID         string `json:"id"`
		Owner      string `json:"owner"`
		Visibility string `json:"visibility"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &begin); err != nil {
		t.Fatal(err)
	}
	if begin.Owner != userID || begin.Visibility != "private" || !id.Valid(begin.ID) {
		t.Fatalf("begin %+v, user %s", begin, userID)
	}
	if len(body) > chunkSize {
		putChunk(t, h, cookie, begin.ID, 1, body[chunkSize:])
		state := do(t, h, http.MethodGet, "/api/uploads/"+begin.ID, cookie, nil, nil)
		if state.Code != http.StatusOK {
			t.Fatalf("state %d %s", state.Code, state.Body.Bytes())
		}
		var got struct {
			Received []int `json:"received"`
			Missing  []int `json:"missing"`
		}
		if err := json.Unmarshal(state.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if !containsInt(got.Received, 1) || !containsInt(got.Missing, 0) {
			t.Fatalf("resume state received %v missing %v", got.Received, got.Missing)
		}
	}
	putChunk(t, h, cookie, begin.ID, 0, body[:min(chunkSize, len(body))])
	sum := sha256.Sum256(body)
	done := postJSON(t, h, "/api/uploads/"+begin.ID+"/complete", cookie, map[string]string{
		"sha256": hex.EncodeToString(sum[:]),
	})
	if done.Code != http.StatusCreated {
		t.Fatalf("complete %d %s", done.Code, done.Body.Bytes())
	}
	return begin.ID
}

func putChunk(t *testing.T, h http.Handler, cookie *http.Cookie, uploadID string, index int, chunk []byte) {
	t.Helper()
	sum := sha256.Sum256(chunk)
	header := make(http.Header)
	header.Set("X-Chunk-SHA256", hex.EncodeToString(sum[:]))
	rec := do(t, h, http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/%d", uploadID, index), cookie, bytes.NewReader(chunk), header)
	if rec.Code != http.StatusOK {
		t.Fatalf("chunk %d: %d %s", index, rec.Code, rec.Body.Bytes())
	}
}

func containsInt(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func loopbackFetch(t *testing.T, rawURL string) fetch.Config {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return fetch.Config{
		AllowHTTP:  true,
		Classifier: func(ip net.IP) bool { return ip != nil && ip.IsLoopback() },
		Ports:      []int{port},
		ContentTypes: []string{
			"text/html",
			"application/xhtml+xml",
			"image/jpeg",
			"image/png",
			"image/webp",
		},
		Log: slog.New(slog.DiscardHandler),
	}
}

func jpegWithEXIF(t *testing.T, width, height, orientation int, description string) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = 180
		img.Pix[i+1] = 40
		img.Pix[i+2] = 50
		img.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if description == "" && orientation == 1 {
		return buf.Bytes()
	}
	return spliceEXIF(buf.Bytes(), exifTIFF(binary.BigEndian, orientation, description))
}

func exifTIFF(order binary.ByteOrder, orientation int, description string) []byte {
	entries := 1
	if description != "" {
		entries = 2
	}
	var b []byte
	b = append(b, 'M', 'M')
	b = appendU16(b, order, 42)
	b = appendU32(b, order, 8)
	b = appendU16(b, order, uint16(entries))
	b = appendU16(b, order, 0x0112)
	b = appendU16(b, order, 3)
	b = appendU32(b, order, 1)
	b = appendU16(b, order, uint16(orientation))
	b = appendU16(b, order, 0)
	if description != "" {
		b = appendU16(b, order, 0x010E)
		b = appendU16(b, order, 2)
		b = appendU32(b, order, uint32(len(description)+1))
		b = appendU32(b, order, uint32(8+2+12*entries+4))
	}
	b = appendU32(b, order, 0)
	if description != "" {
		b = append(b, description...)
		b = append(b, 0)
	}
	return b
}

func spliceEXIF(jpegBytes, tiff []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(payload) + 2
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1, byte(segLen >> 8), byte(segLen)}
	out = append(out, payload...)
	return append(out, jpegBytes[2:]...)
}

func appendU16(b []byte, order binary.ByteOrder, v uint16) []byte {
	var t [2]byte
	order.PutUint16(t[:], v)
	return append(b, t[:]...)
}

func appendU32(b []byte, order binary.ByteOrder, v uint32) []byte {
	var t [4]byte
	order.PutUint32(t[:], v)
	return append(b, t[:]...)
}

func heicPrefix() []byte {
	b := make([]byte, 16)
	binary.BigEndian.PutUint32(b[0:4], 16)
	copy(b[4:8], "ftyp")
	copy(b[8:12], "heic")
	return b
}
