package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

const storePath = "data/seen.json"

type Store struct {
	mu   sync.Mutex
	seen map[string]int64
}

func LoadStore() *Store {
	s := &Store{seen: make(map[string]int64)}
	data, err := os.ReadFile(storePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("读取已通知记录失败: %v", err)
		}
		return s
	}
	if err := json.Unmarshal(data, &s.seen); err != nil {
		log.Printf("解析已通知记录失败: %v", err)
	}
	return s
}

func (s *Store) IsNew(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[id]
	return !ok
}

func (s *Store) Mark(id string) {
	s.mu.Lock()
	s.seen[id] = time.Now().Unix()
	s.mu.Unlock()
	s.persist()
}

func (s *Store) Cleanup(maxAge int64) {
	s.mu.Lock()
	cutoff := time.Now().Unix() - maxAge
	for id, ts := range s.seen {
		if ts < cutoff {
			delete(s.seen, id)
		}
	}
	s.mu.Unlock()
	s.persist()
}

func (s *Store) persist() {
	s.mu.Lock()
	data, err := json.Marshal(s.seen)
	s.mu.Unlock()
	if err != nil {
		log.Printf("序列化已通知记录失败: %v", err)
		return
	}
	if err := os.WriteFile(storePath, data, 0644); err != nil {
		log.Printf("写入已通知记录失败: %v", err)
	}
}
