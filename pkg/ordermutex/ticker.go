package ordermutex

type Ticket interface {
	ID() uint64
}

type ticket uint64

func (t ticket) ID() uint64 { return uint64(t) }
