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
	ID         string `json:"id"`
	CourseName string `json:"courseName"`
	Teacher    string `json:"teacher"`
	Status     string `json:"status"`
}

type Monitor struct {
	client   *http.Client
	store    *Store
	notifier *Notifier
	session  string
}

func NewMonitor(session string, store *Store, notifier *Notifier) *Monitor {
	return &Monitor{
		client: &http.Client{
			Timeout:   15 * time.Second,
			Jar:       nil, // we set cookies manually
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

	for _, rc := range rollcalls {
		if m.store.IsNew(rc.ID) {
			log.Printf("发现新签到: id=%s course=%s teacher=%s status=%s",
				rc.ID, rc.CourseName, rc.Teacher, rc.Status)
			m.store.Mark(rc.ID)
			if err := m.notifier.SendRollcallAlert(
				rc.CourseName, rc.Teacher, rc.Status,
				fmt.Sprintf("ID: %s", rc.ID),
			); err != nil {
				log.Printf("发送邮件通知失败: %v", err)
			} else {
				log.Printf("签到通知已发送: %s", rc.CourseName)
			}
		}
	}

	return nil
}

func parseRollcalls(body []byte) ([]Rollcall, error) {
	// Try direct array first
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

	// Try flexible parsing — find any array and try to extract fields
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
				if rc.ID != "" {
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
						if rc.ID != "" {
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
	if v, ok := m["id"]; ok {
		rc.ID = fmt.Sprint(v)
	}
	if v, ok := m["courseName"]; ok {
		rc.CourseName = fmt.Sprint(v)
	} else if v, ok := m["course_name"]; ok {
		rc.CourseName = fmt.Sprint(v)
	} else if v, ok := m["name"]; ok {
		rc.CourseName = fmt.Sprint(v)
	}
	if v, ok := m["teacher"]; ok {
		rc.Teacher = fmt.Sprint(v)
	} else if v, ok := m["teacherName"]; ok {
		rc.Teacher = fmt.Sprint(v)
	}
	if v, ok := m["status"]; ok {
		rc.Status = fmt.Sprint(v)
	}
	return rc
}
