// Package jdparse implements the "粘贴 JD 链接即可预填" feature (#3): the
// server fetches a job-description URL the user pasted and extracts the
// company / position / location for the create-application dialog.
//
// This is a server-initiated outbound request, so it is treated as an SSRF
// surface and hardened accordingly:
//   - scheme must be http/https only;
//   - the resolved address must not be a loopback / link-local / private /
//     unspecified / multicast / broadcast / reserved address — the hostname is
//     resolved to EVERY IP and checked before dialing (DNS-rebinding safe);
//   - redirects are followed with the SAME per-hop checks (never silently
//     chasing a Location onto an internal host), bounded to 3 hops;
//   - the response body is capped at 2 MiB and the whole request bounded by a
//     timeout;
//   - parsing failures return an empty result with a descriptive error — the
//     caller answers 200 + empty fields so the UI falls back to manual entry
//     instead of a 5xx.
package jdparse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	maxRedirects    = 3
	maxResponseBody = 2 << 20 // 2 MiB of HTML is more than enough for a JD page
	defaultTimeout  = 8 * time.Second
	userAgent       = "OfferLog-JD-Parser/0.1"
)

// Result is the parsed suggestion; empty strings mean "we could not tell".
type Result struct {
	CompanyName string  `json:"company_name"`
	Position    string  `json:"position"`
	Location    string  `json:"location"`
	Confidence  float64 `json:"confidence"`
}

// ParseError is a user-facing failure that must map to 200 + empty fields.
type ParseError struct{ msg string }

func (e *ParseError) Error() string { return e.msg }

func errf(format string, args ...any) error {
	return &ParseError{msg: fmt.Sprintf(format, args...)}
}

// blocked reports whether the IP is in a range the server must never fetch.
func blocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsPrivate()
}

// checkURL validates the host of u (scheme + literal-IP or fully-resolved
// hostname). The transport dialer re-checks at connect time, so a hostname
// that resolves differently on the actual dial still cannot sneak through.
func checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("仅支持 http/https 链接")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("链接缺少主机")
	}
	if ip := net.ParseIP(host); ip != nil {
		if blocked(ip) {
			return fmt.Errorf("不允许访问的地址 %s", host)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("域名解析失败")
	}
	for _, ia := range ips {
		if blocked(ia.IP) {
			return fmt.Errorf("域名解析到内网/保留地址 %s", ia.IP)
		}
	}
	return nil
}

// safeClient returns an HTTP client whose dialer refuses internal/private
// destinations and whose redirect policy re-checks every hop.
func safeClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Client{
		Timeout: defaultTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("重定向次数过多")
			}
			return checkURL(req.URL)
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				if ip := net.ParseIP(host); ip != nil {
					if blocked(ip) {
						return nil, fmt.Errorf("不允许访问的地址 %s", ip)
					}
					return dialer.DialContext(ctx, network, addr)
				}
				// Resolve and validate EVERY address up front so a host that
				// resolves to an internal IP (now or on a later query) is
				// rejected before any connection is attempted.
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				for _, ia := range ips {
					if blocked(ia.IP) {
						return nil, fmt.Errorf("目标解析到内网地址 %s", ia.IP)
					}
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}
}

// Parse fetches urlStr and extracts company/position/location. Any failure —
// bad scheme, internal host, network error, unparseable HTML — returns an
// empty Result plus a descriptive error; never a server error.
func Parse(ctx context.Context, urlStr string) (Result, error) {
	u, err := url.Parse(strings.TrimSpace(urlStr))
	if err != nil {
		return Result{}, errf("链接格式不正确")
	}
	if err := checkURL(u); err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, errf("无法发起请求")
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := safeClient().Do(req)
	if err != nil {
		return Result{}, errf("无法打开链接（网络错误或目标拒绝访问）")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, errf("目标页面返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return Result{}, errf("读取页面失败")
	}
	return extract(string(body), u), nil
}

// titleSeparators cut a page title like "前端工程师 - 字节跳动 | BOSS直聘"
// down to the first meaningful token.
var titleSeparators = []string{" - ", " – ", " — ", " · ", "|"}

// extract runs cheap, deterministic heuristics over the page. When nothing is
// recognizable it returns an empty Result rather than a wrong guess (the
// dialog then falls back to manual entry).
func extract(page string, u *url.URL) Result {
	var r Result
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return r
	}
	title, ogTitle, ogSite, metaCompany, metaPosition, metaLocation := "", "", "", "", "", ""
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if c := n.FirstChild; c != nil && title == "" {
					title = strings.TrimSpace(c.Data)
				}
			case "meta":
				var prop, name, content string
				for _, a := range n.Attr {
					switch strings.ToLower(a.Key) {
					case "property":
						prop = strings.ToLower(a.Val)
					case "name":
						name = strings.ToLower(a.Val)
					case "content":
						content = strings.TrimSpace(a.Val)
					}
				}
				switch {
				case prop == "og:title":
					ogTitle = content
				case prop == "og:site_name":
					ogSite = content
				case name == "twitter:title":
					if ogTitle == "" {
						ogTitle = content
					}
				case name == "jobtitle":
					metaPosition = content
				case name == "hiringorganization" || name == "company":
					metaCompany = content
				case name == "joblocation" || name == "location":
					metaLocation = content
				}
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)

	// Title is usually "岗位 - 公司 | 平台". Prefer og:title, then <title>.
	cand := firstNonEmpty(ogTitle, title)
	for _, sep := range titleSeparators {
		if i := strings.Index(cand, sep); i > 0 {
			cand = cand[:i]
			break
		}
	}
	cand = strings.Trim(strings.TrimSpace(cand), " -–—|·")

	// A title that equals the company is not a position ("字节跳动 - 招聘");
	// treat only non-trivial tokens as position candidates.
	position := cleanField(metaPosition)
	if position == "" && len([]rune(cand)) >= 2 && !strings.EqualFold(cand, metaCompany) && !strings.EqualFold(cand, ogSite) && !strings.EqualFold(cand, hostCompany(u.Hostname())) {
		position = cand
	}
	if position != "" {
		r.Position = position
		r.Confidence += 0.4
	}

	company := cleanField(firstNonEmpty(metaCompany, ogSite))
	if company == "" && position != "" {
		// Only fall back to a hostname guess when the page actually looks like
		// a job ad — a bare company guess from a useless page is worse than
		// leaving the field empty.
		company = cleanField(hostCompany(u.Hostname()))
	}
	if company != "" {
		r.CompanyName = company
		r.Confidence += 0.4
	}

	if loc := cleanField(metaLocation); loc != "" {
		r.Location = loc
		r.Confidence += 0.2
	}
	return r
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// hostCompany guesses the company from a hostname. Known job-platform domains
// are rejected (their hostname is not the employer); bare-looking domains like
// "bytedance.com" or "acme.jobs" map to the registrable label.
func hostCompany(host string) string {
	h := strings.ToLower(host)
	h = strings.TrimPrefix(h, "www.")
	platformHosts := map[string]bool{
		"jobs.smartrecruiters.com": true, "jobs.lever.co": true, "boards.greenhouse.io": true,
		"jobs.ashbyhq.com": true, "careers.recruit.net": true, "www.linkedin.com": true,
		"linkedin.com": true, "indeed.com": true, "www.indeed.com": true, "glassdoor.com": true,
		"www.glassdoor.com": true, "zhaopin.com": true, "www.zhaopin.com": true,
		"51job.com": true, "www.51job.com": true, "liepin.com": true, "www.liepin.com": true,
		"lagou.com": true, "www.lagou.com": true, "bosszhipin.com": true, "www.bosszhipin.com": true,
		"zhipin.com": true, "www.zhipin.com": true, "m.zhipin.com": true,
	}
	if platformHosts[h] {
		return ""
	}
	// "jobs.bytedance.com" / "careers.acme.com" → company label is the next
	// label in; "acme.com" → "acme".
	parts := strings.Split(h, ".")
	for len(parts) > 1 && (parts[0] == "jobs" || parts[0] == "careers" || parts[0] == "recruiting" || parts[0] == "hr") {
		parts = parts[1:]
	}
	if len(parts) >= 2 && (parts[len(parts)-1] == "com" || parts[len(parts)-1] == "cn" ||
		parts[len(parts)-1] == "io" || parts[len(parts)-1] == "net" || parts[len(parts)-1] == "org") {
		return parts[0]
	}
	return ""
}

// cleanField normalizes whitespace and bounds the field length.
func cleanField(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// LooksLikeURL reports whether the pasted text is plausibly an http(s) URL.
// The frontend uses it to decide between prefilling and saving a plain link.
func LooksLikeURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
