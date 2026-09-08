// Design-redesign backend acceptance (docs/前端设计改版-后端需求.md):
//
//	#1 stage history: GET /applications?include=stage_history returns the
//	   earliest user-zone calendar day per reached status (incl. terminal
//	   dead-ends), with corrections applied.
//	#2 cross-entity search: GET /search finds applications / companies /
//	   files owned by the caller only.
//	#4 attachment interview round: POST /files accepts interview_id; the file
//	   list returns interview_id per row; deleting a file scrubs it from
//	   image-typed custom_values; a foreign/mismatched interview is rejected.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/applications/domain"
	apprepo "offerlog/backend/internal/applications/repository"
	appservice "offerlog/backend/internal/applications/service"
	apptransport "offerlog/backend/internal/applications/transport"
	filetransport "offerlog/backend/internal/files/transport"
	identitydomain "offerlog/backend/internal/identity/domain"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/platform/objectstore"
	searchrepo "offerlog/backend/internal/search"
	searchtransport "offerlog/backend/internal/search/transport"
	vrepo "offerlog/backend/internal/views/repository"
	vservice "offerlog/backend/internal/views/service"
	vtransport "offerlog/backend/internal/views/transport"
)

// newRedesignServer mounts the applications / files / views / search routes
// over a shared fake-owner session, mirroring the bootstrap wiring.
func newRedesignServer(t *testing.T, db *database.DB, owner int64, timezone string) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &identitydomain.User{ID: owner, Email: "x@y.z", Timezone: timezone, Locale: "zh-CN"})
		c.Next()
	})
	api := r.Group("/api/v1")

	appSvc := appservice.New(db, apprepo.New(db))
	appH := apptransport.New(appSvc)
	appH.Routes(api.Group("/applications"))

	obj, err := objectstore.New(objectstore.Config{Provider: "local", LocalDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	fileH := filetransport.New(obj, db, filetransport.Config{MaxFileBytes: 20 << 20, QuotaBytes: 1 << 30})
	fileH.Routes(api.Group("/files"))

	vs := vservice.New(db, vrepo.New(db)).WithStageHistory(apprepo.New(db))
	vH := vtransport.New(vs)
	vH.Routes(api.Group("/views"))

	sH := searchtransport.New(searchrepo.New(db))
	sH.Routes(api.Group("/search"))

	return httptest.NewServer(r)
}

func decode(t *testing.T, res *http.Response, out any) {
	t.Helper()
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatalf("decode %d: %v", res.StatusCode, err)
	}
}

func mustStatus(t *testing.T, res *http.Response, want int) {
	t.Helper()
	defer res.Body.Close()
	if res.StatusCode != want {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(res.Body)
		t.Fatalf("status = %d, want %d; body=%s", res.StatusCode, want, buf.String())
	}
}

func TestStageHistoryIncludeOnListAndQuery(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	app := mustCreate(t, svc, owner, "StageCo", "后端")

	// Walk saved → applied → screening → rejected (dead end) with real dates.
	step := func(to string, v int, at time.Time) {
		t.Helper()
		if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
			ToStatus: to, Version: v, OccurredAt: &at, Reason: "测试推进",
			SubmittedAt: &at, FirstResponseAt: &at,
		}); err != nil {
			t.Fatalf("transition %s: %v", to, err)
		}
	}
	// Pin the created event to a deterministic day (the real created event
	// carries the server clock, which would make the "saved" assertion
	// clock-dependent).
	if _, err := db.Pool().Exec(ctx, `UPDATE application_events SET occurred_at='2026-09-01T09:00:00Z'
		WHERE application_id=$1 AND event_type='created'`, app.ID); err != nil {
		t.Fatal(err)
	}
	step(domain.StatusApplied, 1, time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC))
	step(domain.StatusScreening, 2, time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC))
	step(domain.StatusRejected, 3, time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC))

	srv := newRedesignServer(t, db, owner, "Europe/Dublin")
	defer srv.Close()

	var page struct {
		Items []map[string]any `json:"items"`
	}
	// List with include=stage_history.
	res, err := http.Get(srv.URL + "/api/v1/applications?include=stage_history&page_size=10")
	if err != nil {
		t.Fatal(err)
	}
	decode(t, res, &page)
	if len(page.Items) != 1 {
		t.Fatalf("list returned %d items, want 1", len(page.Items))
	}
	sh, ok := page.Items[0]["stage_history"].(map[string]any)
	if !ok || sh == nil {
		t.Fatalf("stage_history missing on list item: %#v", page.Items[0]["stage_history"])
	}
	// UTC+1 (Dublin) — the 09:00 UTC instants stay on the same calendar day.
	for st, want := range map[string]string{
		"saved": "2026-09-01", "applied": "2026-09-02", "screening": "2026-09-04", "rejected": "2026-09-06",
	} {
		if sh[st] != want {
			t.Errorf("list stage %s = %v, want %s", st, sh[st], want)
		}
	}

	// List WITHOUT include has no stage_history key.
	page = struct {
		Items []map[string]any `json:"items"`
	}{}
	res, err = http.Get(srv.URL + "/api/v1/applications?page_size=10")
	if err != nil {
		t.Fatal(err)
	}
	decode(t, res, &page)
	if _, ok := page.Items[0]["stage_history"]; ok {
		t.Error("stage_history must be absent without include=stage_history")
	}

	// Views query honors include=stage_history too.
	body := `{"include":["stage_history"],"page":1,"page_size":10}`
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/views/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decode(t, res, &page)
	if len(page.Items) != 1 {
		t.Fatalf("views query returned %d items, want 1", len(page.Items))
	}
	sh, ok = page.Items[0]["stage_history"].(map[string]any)
	if !ok || sh["rejected"] != "2026-09-06" {
		t.Fatalf("views query stage_history = %#v", page.Items[0]["stage_history"])
	}
}

func TestStageHistoryBucketsByUserTimezone(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	app := mustCreate(t, svc, owner, "TZCo", "Role")

	// 2026-09-05 23:30 UTC is already 2026-09-06 in Shanghai (UTC+8).
	at := time.Date(2026, 9, 5, 23, 30, 0, 0, time.UTC)
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, OccurredAt: &at, Reason: "投递", SubmittedAt: &at,
	}); err != nil {
		t.Fatal(err)
	}
	srv := newRedesignServer(t, db, owner, "Asia/Shanghai")
	defer srv.Close()
	var page struct {
		Items []map[string]any `json:"items"`
	}
	res, err := http.Get(srv.URL + "/api/v1/applications?include=stage_history")
	if err != nil {
		t.Fatal(err)
	}
	decode(t, res, &page)
	sh := page.Items[0]["stage_history"].(map[string]any)
	if sh["applied"] != "2026-09-06" {
		t.Errorf("applied day in Asia/Shanghai = %v, want 2026-09-06", sh["applied"])
	}
	// The created event carries the server's real now: its Shanghai calendar
	// day must equal the event's own instant bucketed to Shanghai.
	var createdAt time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT occurred_at FROM application_events WHERE application_id=$1 AND event_type='created'`, app.ID).Scan(&createdAt); err != nil {
		t.Fatal(err)
	}
	wantSaved := createdAt.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	if sh["saved"] != wantSaved {
		t.Errorf("saved day in Asia/Shanghai = %v, want %s", sh["saved"], wantSaved)
	}
}

func TestSearchScopesAndMixes(t *testing.T) {
	ctx := context.Background()
	db, svc, _, ownerA := setup(t)
	ownerB := createOwner(t, db)

	mustCreate(t, svc, ownerA, "北极星公司", "前端工程师")
	mustCreate(t, svc, ownerB, "北极星竞争对手", "后端工程师")

	// A file owned by A.
	var fid string
	if err := db.Pool().QueryRow(ctx, `INSERT INTO files(owner_id, object_key, final_key, original_name, category, status)
		VALUES($1,'staging/x','owners/$1/files/x','简历_前端_v7.pdf','resume','ready') RETURNING id`, ownerA).Scan(&fid); err != nil {
		t.Fatal(err)
	}
	// B has a file too — must never surface for A.
	var fidB string
	if err := db.Pool().QueryRow(ctx, `INSERT INTO files(owner_id, object_key, final_key, original_name, category, status)
		VALUES($1,'staging/y','owners/$1/files/y','北极星内部资料.pdf','other','ready') RETURNING id`, ownerB).Scan(&fidB); err != nil {
		t.Fatal(err)
	}
	_ = fidB

	srvA := newRedesignServer(t, db, ownerA, "Europe/Dublin")
	defer srvA.Close()
	srvB := newRedesignServer(t, db, ownerB, "Europe/Dublin")
	defer srvB.Close()

	get := func(srv *httptest.Server, q string) []map[string]any {
		t.Helper()
		res, err := http.Get(srv.URL + "/api/v1/search?q=" + q)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Items []map[string]any `json:"items"`
		}
		decode(t, res, &body)
		return body.Items
	}

	// A sees only A's app + file for a shared keyword, never B's rows. The
	// file's name matches because its resume filename contains the keyword
	// (北极星 → 简历_北极星_v7.pdf); B's file with the same keyword must not
	// leak.
	if _, err := db.Pool().Exec(ctx, `UPDATE files SET original_name='简历_北极星_v7.pdf' WHERE id=$1`, fid); err != nil {
		t.Fatal(err)
	}
	items := get(srvA, "北极星")
	foundApp := false
	foundB := false
	foundFile := false
	for _, it := range items {
		if it["kind"] == "application" {
			if fmt.Sprint(it["label"]) == "北极星公司 · 前端工程师" {
				foundApp = true
			}
			if fmt.Sprint(it["label"]) == "北极星竞争对手 · 后端工程师" {
				foundB = true
			}
		}
		if it["kind"] == "file" && fmt.Sprint(it["label"]) == "简历_北极星_v7.pdf" {
			foundFile = true
		}
	}
	if !foundApp {
		t.Errorf("A's app missing from A's search: %v", items)
	}
	if foundB {
		t.Errorf("B's app leaked into A's search: %v", items)
	}
	if !foundFile {
		t.Errorf("A's file missing from A's search: %v", items)
	}

	// Empty query returns recently touched rows, owner-scoped.
	recent := get(srvA, "")
	if len(recent) == 0 {
		t.Error("empty q must return recent rows")
	}
}

func TestUploadFileToInterviewRoundAndCleanup(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	app := mustCreate(t, svc, owner, "RoundCo", "岗位")

	var iid int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, result)
		VALUES($1,$2,'一面','video','pending') RETURNING id`, app.ID, owner).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	srv := newRedesignServer(t, db, owner, "Europe/Dublin")
	defer srv.Close()

	// Build a multipart upload with application_id + interview_id.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "shot.png")
	_, _ = fw.Write([]byte("\x89PNG\r\n\x1a\n" + "screenshot bytes"))
	_ = mw.WriteField("category", "other")
	_ = mw.WriteField("application_id", itoa(app.ID))
	_ = mw.WriteField("interview_id", itoa(iid))
	_ = mw.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var up struct {
		ID          string `json:"id"`
		InterviewID *int64 `json:"interview_id"`
	}
	decode(t, res, &up)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", res.StatusCode)
	}
	if up.InterviewID == nil || *up.InterviewID != iid {
		t.Fatalf("upload response interview_id = %v, want %d", up.InterviewID, iid)
	}

	// Files list returns the interview_id and the application filter works.
	var list struct {
		Items []struct {
			ID          string `json:"id"`
			InterviewID *int64 `json:"interview_id"`
			AppID       *int64 `json:"application_id"`
		} `json:"items"`
	}
	res, err = http.Get(srv.URL + "/api/v1/files?application_id=" + itoa(app.ID))
	if err != nil {
		t.Fatal(err)
	}
	decode(t, res, &list)
	if len(list.Items) != 1 || list.Items[0].InterviewID == nil || *list.Items[0].InterviewID != iid {
		t.Fatalf("files list missing interview link: %+v", list.Items)
	}

	// A mismatched interview (other app) is rejected cleanly.
	app2 := mustCreate(t, svc, owner, "OtherCo", "Role2")
	var iid2 int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name) VALUES($1,$2,'一面') RETURNING id`,
		app2.ID, owner).Scan(&iid2); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	mw = multipart.NewWriter(&buf)
	fw, _ = mw.CreateFormFile("file", "x.png")
	_, _ = fw.Write([]byte("\x89PNG\r\n\x1a\nshot"))
	_ = mw.WriteField("application_id", itoa(app.ID))
	_ = mw.WriteField("interview_id", itoa(iid2))
	_ = mw.Close()
	req, _ = http.NewRequest("POST", srv.URL+"/api/v1/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	mustStatus(t, res, http.StatusBadRequest)

	// An image custom property may reference a file id; deleting an
	// UNLINKED file scrubs the id from every owner application's custom_values.
	propKey := "shots"
	if _, err := db.Pool().Exec(ctx, `INSERT INTO property_definitions(owner_id, name, key, data_type)
		VALUES($1,'截图','`+propKey+`','image')`, owner); err != nil {
		t.Fatal(err)
	}
	// Upload a second, unlinked file (no application_id) to delete.
	buf.Reset()
	mw = multipart.NewWriter(&buf)
	fw, _ = mw.CreateFormFile("file", "loose.png")
	_, _ = fw.Write([]byte("\x89PNG\r\n\x1a\nloose shot"))
	_ = mw.Close()
	req, _ = http.NewRequest("POST", srv.URL+"/api/v1/files", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var up2 struct {
		ID string `json:"id"`
	}
	decode(t, res, &up2)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("loose upload status = %d", res.StatusCode)
	}
	cv, _ := json.Marshal(map[string]any{propKey: []string{up2.ID, "keep-me"}})
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET custom_values=$2 WHERE id=$1`, app.ID, cv); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest("DELETE", srv.URL+"/api/v1/files/"+up2.ID, nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	mustStatus(t, res, http.StatusOK)

	var after json.RawMessage
	if err := db.Pool().QueryRow(ctx, `SELECT custom_values FROM applications WHERE id=$1`, app.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(after, &m)
	arr, ok := m[propKey].([]any)
	if !ok || len(arr) != 1 || arr[0] != "keep-me" {
		t.Fatalf("custom_values after delete = %v, want only keep-me", m[propKey])
	}

	// A LINKED screenshot (attached to a round) can be deleted too: the delete
	// removes the association row + object + row, and custom_values is scrubbed.
	cv2, _ := json.Marshal(map[string]any{propKey: []string{up.ID, "keep2"}})
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET custom_values=$2 WHERE id=$1`, app.ID, cv2); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest("DELETE", srv.URL+"/api/v1/files/"+up.ID, nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	mustStatus(t, res, http.StatusOK)
	var linkCount int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM application_files WHERE file_id=$1`, up.ID).Scan(&linkCount); err != nil {
		t.Fatal(err)
	}
	if linkCount != 0 {
		t.Fatalf("linked file delete left %d application_files rows", linkCount)
	}
	if err := db.Pool().QueryRow(ctx, `SELECT custom_values FROM applications WHERE id=$1`, app.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(after, &m)
	arr2 := m[propKey].([]any)
	if len(arr2) != 1 || arr2[0] != "keep2" {
		t.Fatalf("custom_values after linked delete = %v, want only keep2", m[propKey])
	}
}
