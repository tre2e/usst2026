package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

const ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"

func Login(username, password string) (string, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", fmt.Errorf("创建cookie jar失败: %w", err)
	}

	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	session, err := doLogin(client, username, password)
	if err != nil {
		return "", err
	}
	return session, nil
}

func doLogin(client *http.Client, username, password string) (string, error) {
	// Step 1: Follow redirect chain from target to CAS login page
	casPageURL, err := reachCASLogin(client)
	if err != nil {
		return "", fmt.Errorf("访问一网畅学失败: %w", err)
	}

	// Step 2: POST credentials to CAS and follow redirects to get session
	session, err := authenticateCAS(client, casPageURL, username, password)
	if err != nil {
		return "", fmt.Errorf("CAS认证失败: %w", err)
	}

	return session, nil
}

func reachCASLogin(client *http.Client) (string, error) {
	currentURL := "https://1906.usst.edu.cn/user/index"

	for range 10 {
		req, err := http.NewRequest("GET", currentURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", ua)

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		if !isRedirect(resp.StatusCode) {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if isCASLoginPage(body) {
				return currentURL, nil
			}

			// Maybe we already have a session?
			if s := extractSession(jarFromClient(client), resp.Request.URL); s != "" {
				return "", fmt.Errorf("已有有效session，无需登录: %s", s)
			}

			return "", fmt.Errorf("未预期的页面 (status=%d, url=%s)", resp.StatusCode, currentURL)
		}

		loc := resp.Header.Get("Location")
		resp.Body.Close()

		currentURL = resolveURL(currentURL, loc)
	}

	return "", fmt.Errorf("重定向次数过多")
}

func authenticateCAS(client *http.Client, casPageURL, username, password string) (string, error) {
	// Fetch the CAS login form
	req, err := http.NewRequest("GET", casPageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", ua)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Parse form and build POST
	action, fields := parseCASForm(body, casPageURL)
	fields.Set("username", username)
	fields.Set("password", password)

	actionURL := resolveURL(casPageURL, action)

	postReq, err := http.NewRequest("POST", actionURL, strings.NewReader(fields.Encode()))
	if err != nil {
		return "", err
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("User-Agent", ua)
	postReq.Header.Set("Origin", originFromURL(casPageURL))
	postReq.Header.Set("Referer", casPageURL)

	postResp, err := client.Do(postReq)
	if err != nil {
		return "", err
	}
	defer postResp.Body.Close()

	if !isRedirect(postResp.StatusCode) {
		b, _ := io.ReadAll(postResp.Body)
		return "", fmt.Errorf("CAS返回非重定向 (status=%d): 用户名或密码可能有误, body=%s", postResp.StatusCode, string(b))
	}

	// Follow redirect chain to get the session cookie
	nextURL := postResp.Header.Get("Location")
	return followRedirects(client, nextURL)
}

func followRedirects(client *http.Client, startURL string) (string, error) {
	currentURL := startURL

	for range 10 {
		req, err := http.NewRequest("GET", currentURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", ua)

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		if !isRedirect(resp.StatusCode) {
			resp.Body.Close()
			// Check for session cookie
			if s := extractSession(jarFromClient(client), resp.Request.URL); s != "" {
				return s, nil
			}
			return "", fmt.Errorf("认证后未获取到session cookie (url=%s)", currentURL)
		}

		loc := resp.Header.Get("Location")
		resp.Body.Close()
		currentURL = resolveURL(currentURL, loc)
	}

	return "", fmt.Errorf("认证后重定向次数过多")
}

func isRedirect(code int) bool {
	return code == 301 || code == 302 || code == 303 || code == 307 || code == 308
}

func isCASLoginPage(body []byte) bool {
	s := string(body)
	return strings.Contains(s, "ids6.usst.edu.cn") ||
		(strings.Contains(s, "authserver") && strings.Contains(s, "password"))
}

func extractSession(jar *cookiejar.Jar, u *url.URL) string {
	if jar == nil {
		return ""
	}
	for _, c := range jar.Cookies(u) {
		if c.Name == "session" {
			return c.Value
		}
	}
	return ""
}

func jarFromClient(client *http.Client) *cookiejar.Jar {
	if client == nil || client.Jar == nil {
		return nil
	}
	j, _ := client.Jar.(*cookiejar.Jar)
	return j
}

func parseCASForm(body []byte, pageURL string) (action string, fields url.Values) {
	fields = url.Values{}
	action = "/authserver/login"

	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return
	}

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "form" {
				for _, attr := range n.Attr {
					if attr.Key == "action" {
						action = attr.Val
					}
				}
			}
			if n.Data == "input" {
				var name, val string
				for _, attr := range n.Attr {
					switch attr.Key {
					case "name":
						name = attr.Val
					case "value":
						val = attr.Val
					}
				}
				if name != "" && name != "username" && name != "password" {
					fields.Set(name, val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	return
}

func resolveURL(base, ref string) string {
	if ref == "" {
		return base
	}
	u, err := url.Parse(ref)
	if err != nil {
		return base
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return ref
	}
	return baseURL.ResolveReference(u).String()
}

func originFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
}
