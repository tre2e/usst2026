package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const rollcallsURL = "https://1906.usst.edu.cn/api/radar/rollcalls?api_version=1.1.0"

type Rollcall struct {
	RollcallID     int    `json:"rollcall_id"`
	CourseTitle    string `json:"course_title"`
	CreatedByName  string `json:"created_by_name"`
	RollcallStatus string `json:"rollcall_status"`
	RollcallTime   string `json:"rollcall_time"`
	Title          string `json:"title"`
	Type           string `json:"type"`
	IsExpired      bool   `json:"is_expired"`
	StudentStatus  string `json:"status"`
	Source         string `json:"source"`
}

type Monitor struct {
	client     *http.Client
	store      *Store
	notifier   *Notifier
	session    string
	pollCount  int64
}

func NewMonitor(session string, store *Store, notifier *Notifier) *Monitor {
	return &Monitor{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		store:    store,
		notifier: notifier,
		session:  session,
	}
}

func (m *Monitor) UpdateSession(s string) {
	m.session = s
	log.Println("session已更新")
}

func (m *Monitor) Poll() error {
	m.pollCount++
	log.Printf("轮询#%d 开始...", m.pollCount)

	req, err := http.NewRequest("GET", rollcallsURL, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Cookie", fmt.Sprintf("session=%s", m.session))

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 302 || resp.StatusCode == 301 {
		return fmt.Errorf("session可能已过期 (status=%d)", resp.StatusCode)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API返回异常 (status=%d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	rollcalls, err := parseRollcalls(body)
	if err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	newCount := 0
	for _, rc := range rollcalls {
		id := fmt.Sprint(rc.RollcallID)
		if m.store.IsNew(id) {
			newCount++
			log.Printf("发现新签到: id=%d course=%s status=%s student=%s",
				rc.RollcallID, rc.CourseTitle, rc.RollcallStatus, rc.StudentStatus)
			m.store.Mark(id)
			if err := m.notifier.SendRollcallAlert(rc); err != nil {
				log.Printf("发送邮件通知失败: %v", err)
			} else {
				log.Printf("签到通知已发送: %s", rc.CourseTitle)
			}
		}
	}

	// Verbose: first 3 polls show raw response for debugging
	if m.pollCount <= 3 {
		preview := string(body)
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		log.Printf("轮询#%d: 状态=%d, 找到%d个签到, body=%s",
			m.pollCount, resp.StatusCode, len(rollcalls), preview)
	} else if m.pollCount%10 == 0 {
		// After that, heartbeat every 10 polls
		log.Printf("轮询#%d: 找到%d个签到", m.pollCount, len(rollcalls))
	}

	if len(rollcalls) > 0 && newCount == 0 {
		log.Printf("轮询到%d个已通知过的签到，跳过 (seen.json中有记录)", len(rollcalls))
	}

	return nil
}

func parseRollcalls(body []byte) ([]Rollcall, error) {
	// Try direct array (actual API format)
	var arr []Rollcall
	if json.Unmarshal(body, &arr) == nil {
		return arr, nil
	}

	// Try {"data": [...]}
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &wrapper) == nil && wrapper.Data != nil {
		if json.Unmarshal(wrapper.Data, &arr) == nil {
			return arr, nil
		}
	}

	// Try flexible parsing as fallback
	var raw any
	if json.Unmarshal(body, &raw) != nil {
		return nil, fmt.Errorf("无法解析JSON")
	}
	return extractRollcalls(raw), nil
}

func extractRollcalls(v any) []Rollcall {
	var result []Rollcall
	switch val := v.(type) {
	case []any:
		for _, item := range val {
			if m, ok := item.(map[string]any); ok {
				rc := mapToRollcall(m)
				if rc.RollcallID != 0 {
					result = append(result, rc)
				}
			}
		}
	case map[string]any:
		for _, item := range val {
			if arr, ok := item.([]any); ok {
				for _, elem := range arr {
					if m, ok := elem.(map[string]any); ok {
						rc := mapToRollcall(m)
						if rc.RollcallID != 0 {
							result = append(result, rc)
						}
					}
				}
			}
		}
	}
	return result
}

func mapToRollcall(m map[string]any) Rollcall {
	rc := Rollcall{}
	if v, ok := m["rollcall_id"]; ok {
		rc.RollcallID = toInt(v)
	} else if v, ok := m["id"]; ok {
		rc.RollcallID = toInt(v)
	}
	if v, ok := m["course_title"]; ok {
		rc.CourseTitle = fmt.Sprint(v)
	} else if v, ok := m["courseName"]; ok {
		rc.CourseTitle = fmt.Sprint(v)
	}
	if v, ok := m["created_by_name"]; ok {
		rc.CreatedByName = fmt.Sprint(v)
	} else if v, ok := m["teacher"]; ok {
		rc.CreatedByName = fmt.Sprint(v)
	}
	if v, ok := m["rollcall_status"]; ok {
		rc.RollcallStatus = fmt.Sprint(v)
	} else if v, ok := m["status"]; ok {
		rc.RollcallStatus = fmt.Sprint(v)
	}
	if v, ok := m["rollcall_time"]; ok {
		rc.RollcallTime = fmt.Sprint(v)
	}
	if v, ok := m["title"]; ok {
		rc.Title = fmt.Sprint(v)
	}
	if v, ok := m["type"]; ok {
		rc.Type = fmt.Sprint(v)
	}
	if v, ok := m["is_expired"]; ok {
		rc.IsExpired = toBool(v)
	}
	if v, ok := m["source"]; ok {
		rc.Source = fmt.Sprint(v)
	}
	return rc
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		var i int
		fmt.Sscan(n, &i)
		return i
	}
	return 0
}

func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true" || b == "1"
	}
	return false
}
