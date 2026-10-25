package mailer

import (
	"fmt"
	"log"
	"sync"
)

type MockMailer struct {
	mu       sync.Mutex
	messages []Message
	failFor  map[string]error
}

// ConsoleMockMailer is an alias for MockMailer (US-7.1).
type ConsoleMockMailer = MockMailer

func NewMockMailer() *MockMailer {
	return &MockMailer{
		failFor: make(map[string]error),
	}
}

func NewConsoleMockMailer() *ConsoleMockMailer {
	return NewMockMailer()
}

func (m *MockMailer) SetFailFor(email string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		delete(m.failFor, email)
	} else {
		m.failFor[email] = err
	}
}

func (m *MockMailer) Send(msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err, ok := m.failFor[msg.To]; ok {
		return err
	}
	m.messages = append(m.messages, msg)
	log.Printf("[MOCK EMAIL] To: %s | Subject: %s\n%s\n%s", msg.To, msg.Subject, msg.Body, "----------------------------------------")
	return nil
}

func (m *MockMailer) SendBatch(msgs []Message) []error {
	errs := make([]error, len(msgs))
	for i, msg := range msgs {
		errs[i] = m.Send(msg)
	}
	return errs
}

func (m *MockMailer) SentMessages() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Message, len(m.messages))
	copy(out, m.messages)
	return out
}

func (m *MockMailer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = nil
	m.failFor = make(map[string]error)
}

func (m *MockMailer) String() string {
	return fmt.Sprintf("MockMailer(%d messages)", len(m.SentMessages()))
}
