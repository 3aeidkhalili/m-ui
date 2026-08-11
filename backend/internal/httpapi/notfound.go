package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"multivpn/internal/services"
)

// Wrong-address tarpit: the first wrong address shows a friendly 404 page; once
// an IP reaches notFoundStrikeLimit wrong hits it is blocked for notFoundBlockDur
// and every request from it is answered with a live countdown page until the
// block expires. This throttles subscription-token guessing / path scanners.
const (
	notFoundStrikeLimit = 2               // wrong hits before the IP is blocked
	notFoundBlockDur    = 3 * time.Hour   // block duration
	notFoundStrikeTTL   = 30 * time.Minute // idle time after which stale strikes reset
)

type nfEntry struct {
	strikes      int
	last         time.Time
	blockedUntil time.Time
}

type notFoundGuard struct {
	mu sync.Mutex
	m  map[string]*nfEntry
}

func newNotFoundGuard() *notFoundGuard { return &notFoundGuard{m: map[string]*nfEntry{}} }

// blockedRemaining returns the seconds left on an IP's block, or 0 if not blocked.
func (g *notFoundGuard) blockedRemaining(ip string, now time.Time) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.m[ip]
	if e == nil || !now.Before(e.blockedUntil) {
		return 0
	}
	return int(e.blockedUntil.Sub(now).Seconds()) + 1
}

// hit records a wrong-address hit and returns the block-remaining seconds when
// the IP is now (or already) blocked (else 0), plus whether this call is what
// tripped a fresh block.
func (g *notFoundGuard) hit(ip string, now time.Time) (remaining int, justBlocked bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.m) > 50000 { // bound memory against a spoofed-IP flood
		g.m = map[string]*nfEntry{}
	}
	e := g.m[ip]
	if e == nil {
		e = &nfEntry{}
		g.m[ip] = e
	}
	if now.Before(e.blockedUntil) {
		return int(e.blockedUntil.Sub(now).Seconds()) + 1, false
	}
	if now.Sub(e.last) > notFoundStrikeTTL {
		e.strikes = 0
	}
	e.strikes++
	e.last = now
	if e.strikes >= notFoundStrikeLimit {
		e.blockedUntil = now.Add(notFoundBlockDur)
		e.strikes = 0
		return int(notFoundBlockDur.Seconds()), true
	}
	return 0, false
}

// tarpit answers every request from a currently-blocked IP with the countdown
// page, before any route runs.
func (s *Server) tarpit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rem := s.nfGuard.blockedRemaining(s.clientIP(r), time.Now()); rem > 0 {
			s.serveBlockPage(w, rem)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// notFound handles a browser-facing wrong address: it records a strike and shows
// either the friendly 404 page or (once the limit is hit) the countdown page.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if rem, justBlocked := s.nfGuard.hit(ip, time.Now()); rem > 0 {
		if justBlocked {
			services.AuditLog(s.db, services.LogTarpit, "critical", ip,
				"IP به‌دلیل تلاش‌های مکرر برای آدرس اشتباه، ۳ ساعت مسدود شد ("+r.URL.Path+")")
		}
		s.serveBlockPage(w, rem)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(notFoundHTML))
}

func (s *Server) serveBlockPage(w http.ResponseWriter, remaining int) {
	nonceB := make([]byte, 12)
	_, _ = rand.Read(nonceB)
	nonce := hex.EncodeToString(nonceB)
	// Relax the CSP for this page only so the countdown script (nonce-gated) runs.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; style-src 'unsafe-inline'; script-src 'nonce-"+nonce+"'; base-uri 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", fmt.Sprintf("%d", remaining))
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(blockHTML(remaining, nonce)))
}

const nfBaseCSS = `
  *{box-sizing:border-box; font-family:"Vazirmatn","Segoe UI",Tahoma,sans-serif}
  html,body{height:100%;margin:0}
  body{font-family:"Vazirmatn","Segoe UI",Tahoma,sans-serif; color:#eef1f8; direction:rtl;
    background:#05070e; display:grid; place-items:center; padding:24px; overflow:hidden;}
  body::before{content:"";position:fixed;inset:-25%;z-index:-2;filter:blur(46px);
    background:radial-gradient(30rem 30rem at 22% 18%,rgba(99,102,241,.30),transparent 60%),
      radial-gradient(26rem 26rem at 80% 12%,rgba(34,211,238,.22),transparent 55%),
      radial-gradient(28rem 28rem at 68% 92%,rgba(168,85,247,.24),transparent 60%);
    animation:aur 20s ease-in-out infinite alternate}
  body::after{content:"";position:fixed;inset:0;z-index:-3;background:linear-gradient(180deg,#0a0f1e,#05070e)}
  @keyframes aur{from{transform:translate3d(-2%,-1%,0) scale(1)}to{transform:translate3d(3%,2%,0) scale(1.1)}}
  .card{max-width:460px;width:100%;text-align:center;padding:38px 30px;border-radius:26px;
    background:rgba(255,255,255,.05);border:1px solid rgba(255,255,255,.10);
    backdrop-filter:blur(20px) saturate(160%);-webkit-backdrop-filter:blur(20px) saturate(160%);
    box-shadow:0 30px 80px -30px rgba(0,0,0,.7); animation:rise .6s cubic-bezier(.2,.7,.3,1) both}
  @keyframes rise{from{opacity:0;transform:translateY(16px)}}
  .emoji{font-size:56px;line-height:1;margin-bottom:14px; animation:float 4s ease-in-out infinite}
  @keyframes float{50%{transform:translateY(-8px)}}
  h1{font-size:24px;font-weight:800;margin:0 0 10px}
  p{color:#aeb8cf;font-size:14px;line-height:2;margin:0 0 8px}
  .warn{color:#fbbf24;font-size:12.5px;margin-top:14px}
  .btn{display:inline-block;margin-top:20px;padding:12px 26px;border-radius:14px;text-decoration:none;font-weight:700;
    color:#04121a;background:linear-gradient(135deg,#22d3ee,#6366f1);box-shadow:0 12px 26px -10px rgba(34,211,238,.7);transition:.16s}
  .btn:hover{transform:translateY(-2px)}
  @media (prefers-reduced-motion:reduce){*{animation:none!important}}
`

const notFoundHTML = `<!doctype html><html lang="fa" dir="rtl"><head>` +
	`<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">` +
	`<title>آدرس اشتباه</title><style>` + nfBaseCSS + `</style></head>` +
	`<body><div class="card">` +
	`<div class="emoji">🧭</div>` +
	`<h1>اشتباه اومدی!</h1>` +
	`<p>این آدرس وجود ندارد یا لینک اشتراک شما معتبر نیست.<br>لطفاً آدرس را بررسی کنید یا از لینک درست استفاده کنید.</p>` +
	`<p class="warn">⚠️ تلاشِ مکرر برای آدرسِ اشتباه، دسترسیِ IP شما را به‌طور موقت محدود می‌کند.</p>` +
	`<a class="btn" href="/">بازگشت به خانه</a>` +
	`</div></body></html>`

func blockHTML(remaining int, nonce string) string {
	css := nfBaseCSS + `
  .count{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; font-size:46px; font-weight:800;
    letter-spacing:3px; margin:18px 0 6px; color:#fff; text-shadow:0 0 26px rgba(251,113,133,.55);
    background:linear-gradient(135deg,#fb7185,#f59e0b);-webkit-background-clip:text;background-clip:text;-webkit-text-fill-color:transparent}
  .bar{height:8px;border-radius:8px;overflow:hidden;background:rgba(255,255,255,.08);margin:16px auto 4px;max-width:280px}
  .bar>i{display:block;height:100%;border-radius:8px;background:linear-gradient(90deg,#fb7185,#f59e0b);transition:width 1s linear}
  .sub{color:#8b95ad;font-size:12px}`
	return `<!doctype html><html lang="fa" dir="rtl"><head>` +
		`<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<title>دسترسی محدود شد</title><style>` + css + `</style></head>` +
		`<body><div class="card">` +
		`<div class="emoji">⛔</div>` +
		`<h1>دسترسی موقتاً محدود شد</h1>` +
		`<p>به‌دلیل تلاش‌های مکرر برای آدرسِ اشتباه، IP شما به مدت ۳ ساعت محدود شده است.</p>` +
		`<div class="count" id="c">۰۳:۰۰:۰۰</div>` +
		`<div class="bar"><i id="b" style="width:100%"></i></div>` +
		`<p class="sub">پس از پایانِ شمارش، دسترسی به‌طور خودکار باز می‌شود.</p>` +
		`</div>` +
		`<script nonce="` + nonce + `">` + blockJS(remaining) + `</script>` +
		`</body></html>`
}

func blockJS(remaining int) string {
	// total is the full block window (for the progress bar); t is remaining.
	total := int(notFoundBlockDur.Seconds())
	return strings.NewReplacer("__T__", itoa(remaining), "__TOTAL__", itoa(total)).Replace(
		`var t=__T__,total=__TOTAL__,el=document.getElementById('c'),bar=document.getElementById('b');
var fa=['۰','۱','۲','۳','۴','۵','۶','۷','۸','۹'];
function toFa(s){return String(s).replace(/[0-9]/g,function(d){return fa[+d]})}
function pad(n){return (n<10?'0':'')+n}
function tick(){
  if(t<=0){location.reload();return}
  var h=Math.floor(t/3600),m=Math.floor((t%3600)/60),s=t%60;
  el.textContent=toFa(pad(h)+':'+pad(m)+':'+pad(s));
  bar.style.width=(t/total*100)+'%';
  t--;
}
tick();setInterval(tick,1000);`)
}
