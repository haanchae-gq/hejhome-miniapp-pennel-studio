package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

/*
구글 OIDC 로그인 — 스튜디오(server/auth.mjs)와 **같은 방식**이다.

Authorization Code Flow · 의존성 0 · 세션은 HMAC 서명 쿠키.

## 도메인 제한은 서버가 한다

구글의 `hd` 파라미터는 **계정 선택 힌트일 뿐 보안 경계가 아니다.** 그래서 토큰을 받은 뒤
서버가 이메일 도메인과 `hd` 클레임을 **직접 검증**한다. 그러지 않으면 아무 구글 계정으로도
광고 콘솔에 들어와 캠페인을 켜고 끌 수 있다.

## id_token 서명 검증을 왜 생략하나

Authorization Code Flow 에서 id_token 은 구글 토큰 엔드포인트에서 **TLS 직결 + client_secret**
으로 받는다. 중간자가 없으므로 서명 검증을 생략할 수 있다(OIDC Core §3.1.3.7 주석).
암묵 흐름(implicit)이었다면 반드시 검증해야 한다 — 여기선 쓰지 않는다.

## 미설정이면?

OIDC env 가 없으면 **개발 모드**다. `ADS_ADMIN_SECRET` 쿼리 게이트로 떨어지고,
그 사실이 화면에 배지로 드러난다 — 로그인이 꺼져 있는 줄 모르고 운영에 올리는 사고를 막는다.
*/

const (
	sessionCookie = "ads_session"
	oauthCookie   = "ads_oauth"
	sessionTTL    = 12 * time.Hour
)

type oidcConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Domain       string
	Secret       []byte
}

func loadOIDC() *oidcConfig {
	id := os.Getenv("ADS_GOOGLE_CLIENT_ID")
	sec := os.Getenv("ADS_GOOGLE_CLIENT_SECRET")
	if id == "" || sec == "" {
		return nil
	}
	dom := os.Getenv("ADS_ALLOWED_DOMAIN")
	if dom == "" {
		dom = "goqual.com"
	}
	s := os.Getenv("ADS_SESSION_SECRET")
	if s == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		s = base64.RawURLEncoding.EncodeToString(b)
	}
	return &oidcConfig{ClientID: id, ClientSecret: sec,
		RedirectURI: os.Getenv("ADS_OIDC_REDIRECT_URI"),
		Domain:      strings.ToLower(dom), Secret: []byte(s)}
}

func (c *oidcConfig) sign(s string) string {
	m := hmac.New(sha256.New, c.Secret)
	m.Write([]byte(s))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// User — 로그인한 사람. 감사 로그의 주체가 된다.
type User struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Exp   int64  `json:"exp"`
}

func (s *Server) sessionUser(r *http.Request) *User {
	if s.OIDC == nil {
		return nil
	}
	ck, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	parts := strings.SplitN(ck.Value, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(s.OIDC.sign(parts[0]))) != 1 {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil
	}
	var u User
	if json.Unmarshal(raw, &u) != nil || time.Now().Unix() > u.Exp {
		return nil
	}
	return &u
}

func (s *Server) redirectURI(r *http.Request) string {
	if s.OIDC.RedirectURI != "" {
		return s.OIDC.RedirectURI
	}
	scheme := "https"
	if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host + "/auth/callback"
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		http.Error(w, "OIDC 미설정 — 개발 모드에서는 ?k=<ADS_ADMIN_SECRET>", 404)
		return
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	state := base64.RawURLEncoding.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: state, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})

	q := url.Values{
		"client_id": {s.OIDC.ClientID}, "redirect_uri": {s.redirectURI(r)},
		"response_type": {"code"}, "scope": {"openid email profile"},
		"state": {state}, "hd": {s.OIDC.Domain}, "prompt": {"select_account"},
		"access_type": {"online"},
	}
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+q.Encode(), http.StatusFound)
}

func (s *Server) authCallback(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		http.NotFound(w, r)
		return
	}
	ck, err := r.Cookie(oauthCookie)
	if err != nil || ck.Value == "" || ck.Value != r.URL.Query().Get("state") {
		http.Error(w, "state 불일치 — 다시 로그인해 주세요.", 400)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "code 없음", 400)
		return
	}

	form := url.Values{
		"code": {code}, "client_id": {s.OIDC.ClientID}, "client_secret": {s.OIDC.ClientSecret},
		"redirect_uri": {s.redirectURI(r)}, "grant_type": {"authorization_code"},
	}
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", form)
	if err != nil {
		http.Error(w, "구글 토큰 교환 실패", 502)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if json.Unmarshal(body, &tok) != nil || tok.IDToken == "" {
		http.Error(w, "id_token 없음", 502)
		return
	}
	parts := strings.Split(tok.IDToken, ".")
	if len(parts) < 2 {
		http.Error(w, "id_token 형식 오류", 502)
		return
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		http.Error(w, "id_token 디코드 실패", 502)
		return
	}
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		HD    string `json:"hd"`
		Ver   bool   `json:"email_verified"`
	}
	_ = json.Unmarshal(payload, &claims)

	// ── 도메인 제한: 서버가 직접 본다 ──
	email := strings.ToLower(claims.Email)
	okDomain := strings.HasSuffix(email, "@"+s.OIDC.Domain)
	okHD := claims.HD == "" || strings.EqualFold(claims.HD, s.OIDC.Domain)
	if !claims.Ver || !okDomain || !okHD {
		http.Error(w, fmt.Sprintf("@%s 계정만 접근할 수 있어요.", s.OIDC.Domain), 403)
		return
	}

	u := User{Email: email, Name: claims.Name, Exp: time.Now().Add(sessionTTL).Unix()}
	j, _ := json.Marshal(u)
	p := base64.RawURLEncoding.EncodeToString(j)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: p + "." + s.OIDC.sign(p),
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		MaxAge: int(sessionTTL.Seconds())})
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/console", http.StatusFound)
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/console", http.StatusFound)
}

// actor — 지금 조작하는 사람. 감사 로그에 남는다.
// OIDC 가 켜져 있으면 실제 이메일, 개발 모드면 'dev'.
func (s *Server) actor(r *http.Request) string {
	if u := s.sessionUser(r); u != nil {
		return u.Email
	}
	return "dev"
}

// guard — 콘솔 접근 판정. OIDC 가 켜져 있으면 세션이 있어야 하고,
// 꺼져 있으면 개발용 시크릿 게이트로 떨어진다.
func (s *Server) guard(w http.ResponseWriter, r *http.Request) bool {
	if s.OIDC != nil {
		if s.sessionUser(r) != nil {
			return true
		}
		http.Redirect(w, r, "/auth/login", http.StatusFound)
		return false
	}
	if authed(r) {
		return true
	}
	http.Error(w, "unauthorized — 개발 모드: ?k=<ADS_ADMIN_SECRET>", 401)
	return false
}
