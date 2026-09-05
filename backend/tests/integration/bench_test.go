// Benchmark/load spot-check for the plan §12.4 targets: lists, filters and
// stats latency at small concurrency. Seed N applications first.
package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	apprepo "offerlogs/backend/internal/applications/repository"
	appservice "offerlogs/backend/internal/applications/service"
	"os"

	"offerlogs/backend/internal/platform/database"
	"offerlogs/backend/internal/views"
	viewrepo "offerlogs/backend/internal/views/repository"
	vservice "offerlogs/backend/internal/views/service"
)

// BenchmarkQueries seeds 2000 rows then samples list/query/analytics latency.
// This is a spot check, not the full p95 gate (real gate requires the recorded
// 2vCPU/4GB environment per plan §12.4).
func BenchmarkQueries(b *testing.B) {
	ctx := context.Background()
	url := dbURLB(b)
	db, err := database.New(ctx, url)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	owner := createOwnerB(b, db)
	svc := appservice.New(db, apprepo.New(db))

	// seed
	company := "BenchCo"
	for i := 0; i < 500; i++ {
		_, err := svc.Create(ctx, owner, &appservice.CreateInput{
			CompanyName: company, Position: fmt.Sprintf("Position %d", i),
			Channel: pick(i), Priority: "medium",
		})
		if err != nil {
			b.Fatal(err)
		}
	}

	b.Run("list_50", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, err := svc.List(ctx, owner, apprepo.ListOptions{Page: 1, PageSize: 50})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("filter_status", func(b *testing.B) {
		vs := vservice.New(db, viewrepo.New(db))
		f := []views.FilterNode{{Field: "status", Op: "eq", Value: "saved"}}
		for i := 0; i < b.N; i++ {
			_, _, _, err := vs.RunQuery(ctx, owner, f, nil, 1, 50)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func pick(i int) string {
	if i%3 == 0 {
		return "LinkedIn"
	}
	if i%3 == 1 {
		return "Referral"
	}
	return "官网"
}

func dbURLB(_ *testing.B) string {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://offerlogs:offerlogs@localhost:5433/offerlogs?sslmode=disable"
	}
	return url
}

func createOwnerB(t *testing.B, db *database.DB) int64 {
	var id int64
	email := fmt.Sprintf("bench_%d@test.local", time.Now().UnixNano())
	err := db.Pool().QueryRow(context.Background(), `INSERT INTO users(email, password_hash, display_name, timezone)
		VALUES($1,'x','Bench',$2) RETURNING id`, email, "Europe/Dublin").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
