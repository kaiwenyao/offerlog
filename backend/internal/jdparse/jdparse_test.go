package jdparse

import (
	"net/url"
	"testing"
)

func TestLooksLikeURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://jobs.bytedance.com/experienced/position/7123", true},
		{"http://example.com/jd", true},
		{"example.com/jd", false},      // no scheme
		{"javascript:alert(1)", false}, // non-http scheme
		{"", false},
		{"https://", false}, // no host
	}
	for _, c := range cases {
		if got := LooksLikeURL(c.in); got != c.want {
			t.Errorf("LooksLikeURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestExtractCompanyFromHost(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"bytedance.com", "bytedance"},
		{"jobs.bytedance.com", "bytedance"},
		{"careers.acme.io", "acme"},
		{"www.example.com", "example"},
		{"www.linkedin.com", ""},     // known platform
		{"jobs.lever.co", ""},        // known platform
		{"boards.greenhouse.io", ""}, // known platform
		{"zhipin.com", ""},           // known platform
		{"localhost:5173", ""},       // no resolvable label
	}
	for _, c := range cases {
		if got := hostCompany(c.host); got != c.want {
			t.Errorf("hostCompany(%q) = %q, want %q", c.host, got, c.want)
		}
	}
}

func TestExtractPositionFromTitleAndMetas(t *testing.T) {
	page := `<!doctype html><html><head><title>前端工程师 - 字节跳动 | BOSS直聘</title>
		<meta property="og:site_name" content="BOSS直聘">
		<meta name="jobLocation" content="上海"></head><body></body></html>`
	u := mustURL(t, "https://www.zhipin.com/job_detail/123.html")
	r := extract(page, u)
	if r.Position == "" {
		t.Fatal("expected a position from title, got empty")
	}
	if r.Position != "前端工程师" {
		t.Errorf("position = %q, want 前端工程师", r.Position)
	}
	if r.Location != "上海" {
		t.Errorf("location = %q, want 上海", r.Location)
	}
	// og:site_name beats a platform hostname guess.
	if r.CompanyName != "BOSS直聘" {
		t.Errorf("company = %q, want BOSS直聘", r.CompanyName)
	}
}

func TestExtractMetaStructuredData(t *testing.T) {
	page := `<html><head><meta name="jobTitle" content="Backend Engineer">
		<meta name="hiringOrganization" content="Acme Corp">
		<meta name="jobLocation" content="Remote"></head></html>`
	u := mustURL(t, "https://acme.com/careers/42")
	r := extract(page, u)
	if r.Position != "Backend Engineer" || r.CompanyName != "Acme Corp" || r.Location != "Remote" {
		t.Errorf("unexpected extract: %+v", r)
	}
}

func TestExtractEmptyPageReturnsEmptyResult(t *testing.T) {
	r := extract("<html><head></head><body><p>nothing here</p></body></html>", mustURL(t, "https://acme.com/x"))
	if r.Position != "" || r.CompanyName != "" || r.Location != "" {
		t.Errorf("expected empty result on useless page, got %+v", r)
	}
}

func TestExtractCutAtPlatformSuffix(t *testing.T) {
	// "岗位 - 公司 | 平台": cut at the FIRST separator (the dash before the
	// company), not at the pipe.
	page := `<html><head><title>资深 Go 开发 - 上海 xx 科技 | 拉勾</title></head></html>`
	u := mustURL(t, "https://www.lagou.com/wn/jobs/123.html")
	r := extract(page, u)
	if r.Position != "资深 Go 开发" {
		t.Errorf("position = %q, want 资深 Go 开发", r.Position)
	}
}
