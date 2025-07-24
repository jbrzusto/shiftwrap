package shiftwrap

import (
	"container/heap"
	//	"log"
)

// ShiftChangeQueue is a priority queue heap for ShiftChange values,
// using ShiftChange.At as the key.
// ShiftChanges can be added using Add(), and removed
// (by service name) using Remove().
// The count of ShiftChange for each service is maintained
type ShiftChangeQueue struct {
	ShiftChanges ShiftChanges
	Count        map[*Service]int
}

func NewShiftChangeQueue() *ShiftChangeQueue {
	return &ShiftChangeQueue{
		ShiftChanges: ShiftChanges{},
		Count:        map[*Service]int{},
	}
}

// Len is part of the implementation of sort.Interface
func (s ShiftChangeQueue) Len() int { return len(s.ShiftChanges) }

// Less is part of the implementation of sort.Interface
func (s ShiftChangeQueue) Less(i, j int) bool {
	return s.ShiftChanges[i].At.Before(s.ShiftChanges[j].At)
}

// Swap is part of the implementation of sort.Interface
func (s ShiftChangeQueue) Swap(i, j int) {
	s.ShiftChanges[i], s.ShiftChanges[j] = s.ShiftChanges[j], s.ShiftChanges[i]
}

// Push is part of the implementation of heap.Interface
func (s *ShiftChangeQueue) Push(x any) {
	se := x.(ShiftChange)
	//	log.Printf("about to push %v on heap %v\n", se, s)
	s.ShiftChanges = append(s.ShiftChanges, se)
	//	log.Printf("heap now %v\n", s)
}

// Pop is part of the implementation of heap.Interface
func (s *ShiftChangeQueue) Pop() any {
	n := len(s.ShiftChanges)
	item := s.ShiftChanges[n-1]
	s.ShiftChanges = s.ShiftChanges[0 : n-1]
	//	log.Printf("popped %v from heap %v\n", item, s)
	return item
}

// Remove removes all ShiftChanges for the given service.
// We do this in-place, since we'll be calling heap.Init() anyway.
func (s *ShiftChangeQueue) Remove(srv *Service) {
	l := len(s.ShiftChanges)
	for i := 0; i < l; {
		sc := s.ShiftChanges[i]
		if sc.Service() == srv {
			// element at this slot is to be removed;
			// copy last slice element to this slot,
			// if not at end of slice
			if i < l-1 {
				s.ShiftChanges[i] = s.ShiftChanges[l-1]
			}
			// shorten nominal slice length by 1
			l--
		} else {
			// keep this element; look at next
			i++
		}
	}
	// trim slice to nominal length
	s.ShiftChanges = s.ShiftChanges[0:l]
	heap.Init(s)
}

// Add adds ShiftChanges to the heap
func (s *ShiftChangeQueue) Add(scs ...ShiftChange) {
	for _, sc := range scs {
		heap.Push(s, sc)
		s.Count[sc.Service()]++
	}
	//	log.Printf("New first heap element is %v\n", s.ShiftChanges[0])
}

// PopFirst removes and returns the "smallest" element of the heap, if there
// is one. In that case, it also returns ok=true.  Otherwise, it returns ok=false
// and rv = ShiftChange{}
func (s *ShiftChangeQueue) PopFirst() (rv ShiftChange, ok bool) {
	if len(s.ShiftChanges) == 0 {
		return
	}
	rv = heap.Pop(s).(ShiftChange)
	s.Count[rv.Service()]--
	ok = true
	return
}

// Head returns a pointer to the "smallest" element of the heap,
// or nil if it is empty.
func (s *ShiftChangeQueue) Head() (rv *ShiftChange) {
	if len(s.ShiftChanges) == 0 {
		return
	}
	rv = &s.ShiftChanges[0]
	return
}

// String represents the heap as a string
func (s *ShiftChangeQueue) String() string {
	return s.ShiftChanges.String()
}
