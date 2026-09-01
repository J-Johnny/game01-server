package idgen

import (
	"errors"
	"sync"
	"time"
)

const (
	NodeBits     = 10
	SequenceBits = 12
	MaxNodeID    = (1 << NodeBits) - 1
	MaxSequence  = (1 << SequenceBits) - 1
)

var DefaultEpoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

type Generator struct {
	nodeID     uint16
	epochMilli int64
	now        func() time.Time
	mu         sync.Mutex
	lastMilli  int64
	sequence   uint16
}

type Parts struct {
	Timestamp time.Time
	NodeID    uint16
	Sequence  uint16
}

func New(nodeID uint16) (*Generator, error) {
	return NewWithClock(nodeID, DefaultEpoch, time.Now)
}

func NewWithClock(nodeID uint16, epoch time.Time, now func() time.Time) (*Generator, error) {
	if nodeID > MaxNodeID {
		return nil, errors.New("id generator node id is out of range")
	}
	if now == nil {
		return nil, errors.New("id generator clock is required")
	}
	return &Generator{nodeID: nodeID, epochMilli: epoch.UTC().UnixMilli(), now: now, lastMilli: -1}, nil
}

func (g *Generator) Next() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	milli := g.currentMilli()
	if milli < g.lastMilli {
		milli = g.lastMilli
	}
	if milli == g.lastMilli {
		if g.sequence == MaxSequence {
			milli = g.waitNextMilli(g.lastMilli)
			g.sequence = 0
		} else {
			g.sequence++
		}
	} else {
		g.sequence = 0
	}
	g.lastMilli = milli
	return uint64(milli)<<(NodeBits+SequenceBits) | uint64(g.nodeID)<<SequenceBits | uint64(g.sequence)
}

func (g *Generator) Parse(id uint64) Parts {
	milli := int64(id >> (NodeBits + SequenceBits))
	nodeID := uint16((id >> SequenceBits) & MaxNodeID)
	sequence := uint16(id & MaxSequence)
	return Parts{Timestamp: time.UnixMilli(g.epochMilli + milli).UTC(), NodeID: nodeID, Sequence: sequence}
}

func (g *Generator) currentMilli() int64 {
	milli := g.now().UTC().UnixMilli() - g.epochMilli
	if milli < 0 {
		return 0
	}
	return milli
}

func (g *Generator) waitNextMilli(previous int64) int64 {
	for {
		time.Sleep(time.Millisecond)
		milli := g.currentMilli()
		if milli > previous {
			return milli
		}
	}
}
