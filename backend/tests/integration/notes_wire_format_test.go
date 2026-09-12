// Regression: the notes handlers used to serialize the repository struct
// directly. It has no JSON tags, so GET /notes returned {"ID":1,"ContentMD":…}
// instead of {"id":1,"content_md":…} — the frontend read n.content_md, got
// undefined, and the note text never rendered on the 岗位 detail page. Every
// note response must go through noteDTO so the wire keys stay snake_case.
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestNotesWireFormatIsSnakeCaseDTO(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	app := mustCreate(t, svc, owner, "NoteCo", "NoteRole")

	srv := newFullActivityServer(t, db, owner)
	defer srv.Close()

	base := srv.URL + "/api/v1/applications/" + itoa(app.ID)

	// Create a note.
	req, _ := http.NewRequest("POST", base+"/notes", strings.NewReader(`{"content_md":"HR 说下周给结果"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create note -> %d, want 201", res.StatusCode)
	}

	// List must expose snake_case keys the frontend expects.
	req, _ = http.NewRequest("GET", base+"/notes", nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list notes -> %d, want 200", res.StatusCode)
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("listed %d notes, want 1", len(payload.Items))
	}
	note := payload.Items[0]
	for _, key := range []string{"id", "application_id", "content_md", "created_at", "updated_at"} {
		if _, ok := note[key]; !ok {
			t.Fatalf("note JSON missing key %q; got keys %v (repo struct leaked into wire format?)", key, note)
		}
	}
	if note["content_md"] != "HR 说下周给结果" {
		t.Fatalf("content_md = %v, want the note text", note["content_md"])
	}

	// PATCH must return the refreshed row, not a zero-time echo.
	nid, _ := note["id"].(float64)
	req, _ = http.NewRequest("PATCH", base+"/notes/"+itoa(int64(nid)), strings.NewReader(`{"content_md":"改口径了"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update note -> %d, want 200", res.StatusCode)
	}
	var updated map[string]any
	if err := json.NewDecoder(res.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated["content_md"] != "改口径了" {
		t.Fatalf("updated content_md = %v, want 改口径了", updated["content_md"])
	}
	created, _ := updated["created_at"].(string)
	if strings.HasPrefix(created, "0001-") {
		t.Fatalf("updated note created_at is zero time: %q (must refetch stored row)", created)
	}

	// And the row is really gone after DELETE.
	req, _ = http.NewRequest("DELETE", base+"/notes/"+itoa(int64(nid)), nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete note -> %d, want 200", res.StatusCode)
	}
	var n int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM notes WHERE application_id=$1`, app.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("note row survived delete: %d left", n)
	}
}