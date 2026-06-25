package approvals

import (
	"context"
	"sync"
)

// MemoryStore is an in-memory implementation of ApprovalStore.
type MemoryStore struct {
	mu       sync.RWMutex
	requests map[string]*ActionRequest
}

// NewMemoryStore creates a new in-memory approval store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		requests: make(map[string]*ActionRequest),
	}
}

func (s *MemoryStore) SaveRequest(ctx context.Context, req *ActionRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[req.ID] = req
	return nil
}

func (s *MemoryStore) GetRequest(ctx context.Context, id string) (*ActionRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	return req, nil
}

func (s *MemoryStore) UpdateRequest(ctx context.Context, req *ActionRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[req.ID] = req
	return nil
}

func (s *MemoryStore) ListPending(ctx context.Context) ([]*ActionRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pending []*ActionRequest
	for _, req := range s.requests {
		if req.Approved == nil {
			pending = append(pending, req)
		}
	}
	return pending, nil
}
