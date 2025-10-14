package ordermutex

import (
	"sync"

	"go.uber.org/atomic"
)

type OrderMutex interface {
	GetTicket() Ticket
	Lock(Ticket)
	Unlock(Ticket)
	ReturnTicket(Ticket)
}

// orderMutex implements a ticket-lock with precise wakeups.
// Invariants:
//   - next >= cur
//   - cur is the ticket currently allowed to acquire the lock
//   - waiters holds at most one entry per ticket, only for tickets >= cur
//   - burned marks tickets that will never lock (canceled)
//
// Wake-ups are per-ticket by closing that ticket's channel.
type orderMutex struct {
	next atomic.Uint64

	mu      sync.Mutex
	cur     uint64
	waiters map[uint64]chan struct{}
	burned  map[uint64]struct{}
}

func New() OrderMutex {
	return &orderMutex{
		waiters: make(map[uint64]chan struct{}),
		burned:  make(map[uint64]struct{}),
	}
}

func (m *orderMutex) GetTicket() Ticket {
	id := m.next.Add(1) - 1
	return ticket(id)
}

func (m *orderMutex) Lock(t Ticket) {
	id := t.ID()

	// Fast path: grab mu, if it's our turn, enter immediately.
	m.mu.Lock()
	if id == m.cur {
		m.mu.Unlock()
		return
	}

	// Otherwise, park on (or create) this ticket's waiter.
	ch, ok := m.waiters[id]
	if !ok {
		ch = make(chan struct{})
		m.waiters[id] = ch
	}
	m.mu.Unlock()

	// Precise blocking on own ticket only.
	<-ch
	// After wake, it is our turn by construction.
}

func (m *orderMutex) Unlock(t Ticket) {
	id := t.ID()
	m.mu.Lock()
	defer m.mu.Unlock()

	// UB
	if id != m.cur {
		panic("Unlock called for a ticket that does not hold the lock")
	}

	// Advance to next live ticket and wake exactly that one (if any).
	m.cur++
	m.advanceAndWakeNext()
}

// ReturnTicket can be called either:
//   - before Lock: cancel the ticket (burn it)
//   - after Unlock: it's effectively a no-op
//
// Any call between Lock and Unlock is UB (caller responsibility).
func (m *orderMutex) ReturnTicket(t Ticket) {
	id := t.ID()

	m.mu.Lock()
	defer m.mu.Unlock()

	// If already passed, nothing to do (allowed for defer after Unlock).
	if id < m.cur {
		return
	}

	// Mark as burned and clean up: if it was the current ticket,
	// keep advancing until a non-burned ticket is found; then wake it.
	m.burned[id] = struct{}{}

	// If the returning ticket was waiting, remove and close its waiter to avoid leaks.
	if ch, ok := m.waiters[id]; ok {
		// Do NOT wake it (it must not proceed) — instead close & delete to release waiter.
		// Closing would wake it; but a burned ticket must not enter Lock. To avoid waking:
		// we just delete without closing; the goroutine will be blocked only if it's in Lock.
		// However, a goroutine that called Lock for a burned ticket is UB by spec.
		delete(m.waiters, id)
		_ = ch // intentionally not closed
	}

	// If returning the current ticket (or a sequence including it), advance.
	m.advanceAndWakeNext()
}

// advanceAndWakeNext advances cur over any burned tickets;
// then if there is a waiter for m.cur, it wakes exactly that waiter.
func (m *orderMutex) advanceAndWakeNext() {
	// Skip burned tickets strictly ahead of (or at) cur.
	for {
		if _, burned := m.burned[m.cur]; !burned {
			break
		}
		delete(m.burned, m.cur)
		m.cur++
	}

	// Wake the exact next waiter, if any.
	if ch, ok := m.waiters[m.cur]; ok {
		delete(m.waiters, m.cur)
		close(ch) // precise wake-up: only this goroutine proceeds
	}
}
