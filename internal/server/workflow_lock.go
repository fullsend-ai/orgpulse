package server

import (
	"sync"
	"time"
)

type WorkflowLock struct {
	SessionID string    `json:"session_id"`
	ExpiresAt time.Time `json:"-"`
}

type WorkflowLockManager struct {
	mu    sync.Mutex
	locks map[int64]WorkflowLock
	ttl   time.Duration
}

func NewWorkflowLockManager(ttl time.Duration) *WorkflowLockManager {
	return &WorkflowLockManager{
		locks: make(map[int64]WorkflowLock),
		ttl:   ttl,
	}
}

func (m *WorkflowLockManager) Acquire(workflowID int64, sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if lock, ok := m.locks[workflowID]; ok {
		if lock.SessionID == sessionID {
			lock.ExpiresAt = now.Add(m.ttl)
			m.locks[workflowID] = lock
			return true
		}
		if now.Before(lock.ExpiresAt) {
			return false
		}
	}
	m.locks[workflowID] = WorkflowLock{SessionID: sessionID, ExpiresAt: now.Add(m.ttl)}
	return true
}

func (m *WorkflowLockManager) Release(workflowID int64, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lock, ok := m.locks[workflowID]; ok && lock.SessionID == sessionID {
		delete(m.locks, workflowID)
	}
}

func (m *WorkflowLockManager) ForceRelease(workflowID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.locks, workflowID)
}

func (m *WorkflowLockManager) IsLocked(workflowID int64) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock, ok := m.locks[workflowID]
	if !ok || time.Now().After(lock.ExpiresAt) {
		if ok {
			delete(m.locks, workflowID)
		}
		return false, ""
	}
	return true, lock.SessionID
}

func (m *WorkflowLockManager) ActiveLocks() map[int64]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	result := make(map[int64]string)
	for id, lock := range m.locks {
		if now.Before(lock.ExpiresAt) {
			result[id] = lock.SessionID
		} else {
			delete(m.locks, id)
		}
	}
	return result
}
