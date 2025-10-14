package ordermutex

import (
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"
)

func TestRandomLocks(t *testing.T) {
	iterations := 1000

	m := New()
	var wg sync.WaitGroup
	var desiredTicket uint64
	burnedTickets := make(map[uint64]bool)
	for i := 0; i != iterations; i++ {
		burnedTickets[uint64(i)] = rand.Intn(100) < 25 // 25% chance to burn the ticket
	}

	for i := 0; i != iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// shuffle the order of execution to simulate random timing
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(1000)))

			// acquire a ticket in the queue
			myTicket := m.GetTicket()
			defer m.ReturnTicket(myTicket)

			fmt.Printf("%v issued\n", myTicket)

			// simulate some work before Lock
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(1000)))

			// burn some tickets randomly before Lock
			if burned := burnedTickets[myTicket.ID()]; burned {
				fmt.Printf("%v burned\n", myTicket)
				return
			}

			// lock the mutex according to the ticket order
			fmt.Printf("%v waiting\n", myTicket)

			m.Lock(myTicket)
			defer m.Unlock(myTicket)

			fmt.Printf("%v locked\n", myTicket)

			// check if the lock was acquired in the correct order
			for burnedTickets[desiredTicket] {
				desiredTicket++
			}
			if myTicket.ID() == desiredTicket {
				desiredTicket++
			} else {
				fmt.Println("FATAL: ordering was broken")
				os.Exit(1)
			}

			// simulate some work in the critical section
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(10)))
			fmt.Printf("%v unlocking\n", myTicket)
		}()
	}
	wg.Wait()
	fmt.Println("finish")
}

func TestLogicSimple(t *testing.T) {
	m := New()
	t0 := m.GetTicket()
	t1 := m.GetTicket()
	t2 := m.GetTicket()
	defer func() {
		m.ReturnTicket(t0)
		m.ReturnTicket(t1)
		m.ReturnTicket(t2)
	}()

	m.Lock(t0)
	m.Unlock(t0)

	m.Lock(t1)
	m.Unlock(t1)

	m.Lock(t2)
	m.Unlock(t2)
}

func TestLogicReversed(t *testing.T) {
	m := New()
	t0 := m.GetTicket()
	t1 := m.GetTicket()
	t2 := m.GetTicket()
	defer func() {
		m.ReturnTicket(t0)
		m.ReturnTicket(t1)
		m.ReturnTicket(t2)
	}()

	doneT0 := make(chan struct{})
	doneT1 := make(chan struct{})
	doneT2 := make(chan struct{})

	go func() {
		m.Lock(t2)
		m.Unlock(t2)
		close(doneT2)
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-doneT2:
		t.Fatal("t2 finished too early")
	default:
	}

	go func() {
		m.Lock(t1)
		m.Unlock(t1)
		close(doneT1)
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-doneT2:
		t.Fatal("t2 finished too early")
	case <-doneT1:
		t.Fatal("t1 finished too early")
	default:
	}

	go func() {
		m.Lock(t0)
		m.Unlock(t0)
		close(doneT0)
	}()

	<-doneT0
	<-doneT1
	<-doneT2
}

// BenchmarkOrderMutexSequential benchmarks sequential Lock/Unlock operations
func BenchmarkOrderMutexSequential(b *testing.B) {
	m := New()
	tickets := make([]Ticket, b.N)

	// Pre-allocate all tickets
	for i := 0; i < b.N; i++ {
		tickets[i] = m.GetTicket()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Lock(tickets[i])
		m.Unlock(tickets[i])
	}
	b.StopTimer()

	// Cleanup
	for i := 0; i < b.N; i++ {
		m.ReturnTicket(tickets[i])
	}
}

// BenchmarkStdMutexSequential benchmarks standard mutex for comparison
func BenchmarkStdMutexSequential(b *testing.B) {
	var m sync.Mutex
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

// BenchmarkOrderMutexContention benchmarks with concurrent goroutines
func BenchmarkOrderMutexContention(b *testing.B) {
	m := New()
	var wg sync.WaitGroup

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := m.GetTicket()
			m.Lock(t)
			m.Unlock(t)
			m.ReturnTicket(t)
		}()
	}
	wg.Wait()
}

// BenchmarkStdMutexContention benchmarks standard mutex contention for comparison
func BenchmarkStdMutexContention(b *testing.B) {
	var m sync.Mutex
	var wg sync.WaitGroup

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Lock()
			m.Unlock()
		}()
	}
	wg.Wait()
}

// BenchmarkOrderMutexWithBurnedTickets benchmarks with some tickets burned
func BenchmarkOrderMutexWithBurnedTickets(b *testing.B) {
	m := New()
	var wg sync.WaitGroup

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			t := m.GetTicket()
			defer m.ReturnTicket(t)

			// Burn every 10th ticket
			if idx%10 == 0 {
				return
			}

			m.Lock(t)
			m.Unlock(t)
		}(i)
	}
	wg.Wait()
}
