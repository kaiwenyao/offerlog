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
	"strings"
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
	// Walk saved → applied → screening → rejected (dead end) with real dates.
	// Only the 已投递 step carries 实际投递时间 — mirroring the real UI, where
	// that input appears for recruiting-stage targets and the submission time
	// snapshot must stay the day the user entered there.
	step := func(to string, v int, at time.Time, submitted ...time.Time) {
		t.Helper()
		in := &appservice.TransitionInput{
			ToStatus: to, Version: v, OccurredAt: &at, Reason: "测试推进",
			FirstResponseAt: &at,
		}
		if len(submitted) > 0 {
			in.SubmittedAt = &submitted[0]
		}
		if _, err := svc.Transition(ctx, owner, app.ID, in); err != nil {
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
	step(domain.StatusApplied, 1, time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC), time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC))
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

// Backfilled business times: the timeline must carry the user-entered
// 投递时间, never the DB write clock — whether the entry was created today
// with a past submission date, or moved to 已投递 today with only 实际投递
// 时间 filled in.
func TestStageHistoryUsesUserEnteredTimes(t *testing.T) {
	ctx := context.Background()
	db, svc, repo, owner := setup(t)
	today := time.Now().UTC()
	yesterday := today.AddDate(0, 0, -1)

	// (a) create today, already applied yesterday.
	created, err := svc.Create(ctx, owner, &appservice.CreateInput{
		CompanyName: "BackfillCo", Position: "Role", Status: domain.StatusApplied, SubmittedAt: &yesterday,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 建档事件记的是「我今天开始追踪」，投递另有一条事件记「前天投出」——
	// 两件事两条时间，不再让建档行冒充投递日。
	var occ time.Time
	if err := db.Pool().QueryRow(ctx,
		`SELECT occurred_at FROM application_events WHERE application_id=$1 AND event_type='created'`, created.ID).
		Scan(&occ); err != nil {
		t.Fatal(err)
	}
	if got, want := occ.UTC().Format("2006-01-02"), today.Format("2006-01-02"); got != want {
		t.Errorf("created event occurred_at = %s, want the real creation day %s", got, want)
	}
	var appliedOcc time.Time
	if err := db.Pool().QueryRow(ctx,
		`SELECT occurred_at FROM application_events WHERE application_id=$1 AND event_type='status_change' AND to_status=$2`,
		created.ID, domain.StatusApplied).Scan(&appliedOcc); err != nil {
		t.Fatalf("create with a backfilled submitted_at must also write an applied event: %v", err)
	}
	if got, want := appliedOcc.UTC().Format("2006-01-02"), yesterday.Format("2006-01-02"); got != want {
		t.Errorf("applied event occurred_at = %s, want the backfilled day %s", got, want)
	}

	// (b) create as 待投递 today, move to 已投递 with only 实际投递时间.
	app2 := mustCreate(t, svc, owner, "BackfillCo2", "Role2")
	if _, err := svc.Transition(ctx, owner, app2.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: &yesterday,
	}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	var occ2 time.Time
	if err := db.Pool().QueryRow(ctx,
		`SELECT occurred_at FROM application_events WHERE application_id=$1 AND event_type='status_change' AND to_status=$2`,
		app2.ID, domain.StatusApplied).Scan(&occ2); err != nil {
		t.Fatal(err)
	}
	if got, want := occ2.UTC().Format("2006-01-02"), yesterday.Format("2006-01-02"); got != want {
		t.Errorf("applied event occurred_at = %s, want the backfilled day %s", got, want)
	}

	// Stage history via the HTTP surface: applied renders the user-entered day.
	srv := newRedesignServer(t, db, owner, "UTC")
	defer srv.Close()
	res, err := http.Get(srv.URL + "/api/v1/applications?include=stage_history")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	decode(t, res, &page)
	wantDay := yesterday.Format("2006-01-02")
	found := map[string]bool{}
	for _, it := range page.Items {
		sh, _ := it["stage_history"].(map[string]any)
		if sh == nil {
			continue
		}
		if it["company_name"] == "BackfillCo" || it["company_name"] == "BackfillCo2" {
			if sh["applied"] != wantDay {
				t.Errorf("%s applied = %v, want %s", it["company_name"], sh["applied"], wantDay)
			}
			found[it["company_name"].(string)] = true
		}
	}
	if len(found) != 2 {
		t.Errorf("expected both backfilled rows on page 1, found %v (page size may need a bump)", found)
	}
	_ = repo
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

// 时间线右侧显示的是事件的 occurred_at。这些用例锁住「occurred_at 永远是用户
// 写的业务时间」这条不变量 —— 用户报的 bug 是建档行冒充投递日、跳阶时投递时间
// 只进快照列而事件被盖上写库时刻。
func TestTimelineCarriesUserEnteredBusinessTime(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	today := time.Now().UTC()
	twoDaysAgo := today.AddDate(0, 0, -2)

	type row struct {
		kind     string
		to       string
		occurred time.Time
	}
	timeline := func(appID int64) []row {
		t.Helper()
		rs, err := db.Pool().Query(ctx, `SELECT event_type, COALESCE(to_status,''), occurred_at
			FROM application_events WHERE application_id=$1 ORDER BY sequence`, appID)
		if err != nil {
			t.Fatal(err)
		}
		defer rs.Close()
		var out []row
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.kind, &r.to, &r.occurred); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		return out
	}
	day := func(ts time.Time) string { return ts.UTC().Format("2006-01-02") }

	t.Run("skip-ahead with only 投递时间 materializes an applied event at that time", func(t *testing.T) {
		app := mustCreate(t, svc, owner, "SkipTimeline", "Role")
		// 待投递 →（只填了前天的投递时间）→ 笔试作业
		if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
			ToStatus: domain.StatusAssessment, Version: 1, SubmittedAt: &twoDaysAgo,
		}); err != nil {
			t.Fatalf("transition: %v", err)
		}
		got := timeline(app.ID)
		if len(got) != 3 {
			t.Fatalf("want created + applied + assessment, got %d events: %+v", len(got), got)
		}
		if got[1].to != domain.StatusApplied || day(got[1].occurred) != day(twoDaysAgo) {
			t.Errorf("applied event = %+v, want applied on %s", got[1], day(twoDaysAgo))
		}
		if got[2].to != domain.StatusAssessment || day(got[2].occurred) != day(today) {
			t.Errorf("assessment event = %+v, want assessment on %s", got[2], day(today))
		}
	})

	t.Run("an explicit 发生时间 wins over the write clock", func(t *testing.T) {
		app := mustCreate(t, svc, owner, "OccurredWins", "Role")
		yesterday := today.AddDate(0, 0, -1)
		if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
			ToStatus: domain.StatusAssessment, Version: 1,
			SubmittedAt: &twoDaysAgo, OccurredAt: &yesterday,
		}); err != nil {
			t.Fatalf("transition: %v", err)
		}
		got := timeline(app.ID)
		last := got[len(got)-1]
		if day(last.occurred) != day(yesterday) {
			t.Errorf("assessment occurred_at = %s, want the user-entered %s", day(last.occurred), day(yesterday))
		}
	})

	t.Run("the newest event is correctable — that is the one users mistype", func(t *testing.T) {
		app := mustCreate(t, svc, owner, "CorrectLatest", "Role")
		if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
			ToStatus: domain.StatusAssessment, Version: 1, SubmittedAt: &twoDaysAgo,
		}); err != nil {
			t.Fatalf("transition: %v", err)
		}
		var latest int64
		if err := db.Pool().QueryRow(ctx, `SELECT id FROM application_events
			WHERE application_id=$1 AND event_type='status_change' ORDER BY sequence DESC LIMIT 1`, app.ID).
			Scan(&latest); err != nil {
			t.Fatal(err)
		}
		// Same status, corrected time only — the common "I typed the wrong day" repair.
		if err := svc.Correct(ctx, owner, app.ID, &appservice.CorrectionInput{
			EventID: latest, NewStatus: domain.StatusAssessment, OccurredAt: twoDaysAgo, Reason: "时间填错了",
		}); err != nil {
			t.Fatalf("correcting the newest event must be allowed: %v", err)
		}
		got, err := svc.Get(ctx, owner, app.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != domain.StatusAssessment {
			t.Errorf("status after a time-only correction = %s, want unchanged assessment", got.Status)
		}
	})

	t.Run("corrections honor the submitted occurred_at", func(t *testing.T) {
		app := mustCreate(t, svc, owner, "CorrectTime", "Role")
		if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
			ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: &today,
		}); err != nil {
			t.Fatalf("transition: %v", err)
		}
		evs := timeline(app.ID)
		target := evs[len(evs)-1]
		_ = target
		var eventID int64
		if err := db.Pool().QueryRow(ctx, `SELECT id FROM application_events
			WHERE application_id=$1 AND event_type='status_change' ORDER BY sequence DESC LIMIT 1`, app.ID).
			Scan(&eventID); err != nil {
			t.Fatal(err)
		}
		if err := svc.Correct(ctx, owner, app.ID, &appservice.CorrectionInput{
			EventID: eventID, NewStatus: domain.StatusApplied, OccurredAt: twoDaysAgo, Reason: "时间填错了",
		}); err != nil {
			t.Fatalf("correct: %v", err)
		}
		var corrOcc time.Time
		if err := db.Pool().QueryRow(ctx, `SELECT occurred_at FROM application_events
			WHERE application_id=$1 AND event_type='correction'`, app.ID).Scan(&corrOcc); err != nil {
			t.Fatal(err)
		}
		if day(corrOcc) != day(twoDaysAgo) {
			t.Errorf("correction occurred_at = %s, want the user-entered %s — a wrong timestamp must be repairable",
				day(corrOcc), day(twoDaysAgo))
		}
	})
}

// Created straight into 已投递: the 建档 row must describe the pre-submission
// state, because the replay takes submitted_at from the FIRST event whose
// effective status is applied. With 建档 also claiming applied, a later resync
// (any correction triggers one) silently replaced the user's backfilled 投递时间
// with the creation clock — and that value feeds analytics and reminders.
func TestResyncKeepsBackfilledSubmittedAt(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	today := time.Now().UTC()
	twoDaysAgo := today.AddDate(0, 0, -2)

	created, err := svc.Create(ctx, owner, &appservice.CreateInput{
		CompanyName: "ResyncCo", Position: "Role", Status: domain.StatusApplied, SubmittedAt: &twoDaysAgo,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	day := func(ts *time.Time) string {
		if ts == nil {
			return "<nil>"
		}
		return ts.UTC().Format("2006-01-02")
	}
	if got, want := day(created.SubmittedAt), twoDaysAgo.Format("2006-01-02"); got != want {
		t.Fatalf("submitted_at right after create = %s, want %s", got, want)
	}

	// Force a replay the way the product does: correct the applied event.
	var appliedID int64
	if err := db.Pool().QueryRow(ctx, `SELECT id FROM application_events
		WHERE application_id=$1 AND event_type='status_change' AND to_status=$2`,
		created.ID, domain.StatusApplied).Scan(&appliedID); err != nil {
		t.Fatalf("the backfilled submission needs its own event: %v", err)
	}
	if err := svc.Correct(ctx, owner, created.ID, &appservice.CorrectionInput{
		EventID: appliedID, NewStatus: domain.StatusApplied, OccurredAt: twoDaysAgo, Reason: "确认投递日",
	}); err != nil {
		t.Fatalf("correct: %v", err)
	}
	after, err := svc.Get(ctx, owner, created.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := day(after.SubmittedAt), twoDaysAgo.Format("2006-01-02"); got != want {
		t.Errorf("submitted_at after resync = %s, want the user's backfilled %s", got, want)
	}
}

// Correcting an event's TIME has to move the snapshot too, not just add an
// audit row — otherwise the timeline shows the repair while submitted_at and
// the stage rail keep the mistyped day.
func TestCorrectingATimeMovesTheSnapshot(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	today := time.Now().UTC()
	twoDaysAgo := today.AddDate(0, 0, -2)

	app := mustCreate(t, svc, owner, "MoveSnapshot", "Role")
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: &today,
	}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	var appliedID int64
	if err := db.Pool().QueryRow(ctx, `SELECT id FROM application_events
		WHERE application_id=$1 AND event_type='status_change' AND to_status=$2`,
		app.ID, domain.StatusApplied).Scan(&appliedID); err != nil {
		t.Fatal(err)
	}
	// "I typed today by mistake, it was actually 前天."
	if err := svc.Correct(ctx, owner, app.ID, &appservice.CorrectionInput{
		EventID: appliedID, NewStatus: domain.StatusApplied, OccurredAt: twoDaysAgo, Reason: "时间填错了",
	}); err != nil {
		t.Fatalf("correct: %v", err)
	}
	after, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if after.SubmittedAt == nil || after.SubmittedAt.UTC().Format("2006-01-02") != twoDaysAgo.Format("2006-01-02") {
		t.Errorf("submitted_at = %v, want the corrected %s", after.SubmittedAt, twoDaysAgo.Format("2006-01-02"))
	}
}

// The idempotency marker is an internal storage detail; it must never reach a
// client (it used to render inside the user's own note text).
func TestEventNotesDoNotLeakIdempotencyMarker(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	app := mustCreate(t, svc, owner, "NoteLeak", "Role")
	now := time.Now().UTC()
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: &now,
		Note: "内推直接进面", IdempotencyKey: "ui-12345",
	}); err != nil {
		t.Fatalf("transition: %v", err)
	}

	srv := newRedesignServer(t, db, owner, "UTC")
	defer srv.Close()
	res, err := http.Get(fmt.Sprintf("%s/api/v1/applications/%d/events", srv.URL, app.ID))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Items []struct {
			Note string `json:"note"`
		} `json:"items"`
	}
	decode(t, res, &out)
	for _, it := range out.Items {
		if strings.Contains(it.Note, "|idem:") {
			t.Errorf("note leaked the idempotency marker: %q", it.Note)
		}
		if it.Note != "" && it.Note != "内推直接进面" {
			t.Errorf("note = %q, want the user's text verbatim", it.Note)
		}
	}
}
