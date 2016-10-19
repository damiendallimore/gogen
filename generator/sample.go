package generator

import (
	"bytes"
	"math"
	"sync"

	"github.com/coccyx/gogen/internal"
	log "github.com/coccyx/gogen/logger"
)

var bp sync.Pool

func init() {
	bp = sync.Pool{
		New: func() interface{} {
			bb := bytes.NewBuffer([]byte{})
			return bb
		},
	}
}

type sample struct{}

func (foo sample) Gen(item *config.GenQueueItem) error {
	s := item.S
	if item.Count == -1 {
		item.Count = len(s.Lines)
	}
	// log.Debugf("Gen Queue Item %#v", item)
	// outstr := []map[string]string{{"_raw": fmt.Sprintf("%#v", item)}}

	// log.Debugf("Generating sample '%s' with count %d, et: '%s', lt: '%s', SinglePass: %v", s.Name, item.Count, item.Earliest, item.Latest, s.SinglePass)
	// startTime := time.Now()

	if s.SinglePass {
		return genSinglePass(item)
	}
	return genMultiPass(item)
}

func genSinglePass(item *config.GenQueueItem) error {
	s := item.S
	slen := len(s.BrokenLines)

	if slen > 0 {
		events := make([]map[string]string, 0, item.Count)

		if s.RandomizeEvents {
			// log.Debugf("Random filling events for sample '%s' with %d events", s.Name, item.Count)

			for i := 0; i < item.Count; i++ {
				events = append(events, getBrokenEvent(item, item.Rand.Intn(slen)))
				// events[i] = getBrokenEvent(item, item.Rand.Intn(slen))
			}
		} else {
			if item.Count <= slen {
				for i := 0; i < item.Count; i++ {
					// log.Debugf("Count <= sample len, filling with sample '%s' for %d events", s.Name, item.Count)
					events = append(events, getBrokenEvent(item, i))
					// events[i] = getBrokenEvent(item, i)
				}
			} else {
				iters := int(math.Ceil(float64(item.S.Count) / float64(slen)))
				// log.Debugf("Sequentially filling events for sample '%s' of size %d with %d events over %d iterations", s.Name, slen, item.Count, iters)
				for i := 0; i < iters; i++ {
					var count int
					// start := i * slen
					if i == iters-1 {
						count = (item.S.Count - (i * slen))
					} else {
						count = slen
					}
					// log.Debugf("Appending %d events from lines, length %d", count, slen)
					// end := (i * slen) + count
					for j := 0; j < count; j++ {
						events = append(events, getBrokenEvent(item, j))
						// events[j] = getBrokenEvent(item, j)
					}
				}
			}
		}
		outitem := &config.OutQueueItem{S: item.S, Events: events}
		item.OQ <- outitem
	}
	return nil
}

func getBrokenEvent(item *config.GenQueueItem, i int) map[string]string {
	s := item.S
	ret := make(map[string]string, len(s.BrokenLines[i]))
	choices := make(map[int]*int)
	for k, v := range s.BrokenLines[i] {
		event := bp.Get().(*bytes.Buffer)
		event.Reset()
		for _, st := range v {
			if st.T == nil {
				event.WriteString(st.S)
			} else {
				var choice *int
				if choices[st.T.Group] != nil {
					choice = choices[st.T.Group]
				} else {
					choice = new(int)
					*choice = -1
				}
				replacement, err := st.T.GenReplacement(choice, item.Earliest, item.Latest, item.Rand)
				if err != nil {
					log.Errorf("Error generating replacement for token '%s' in sample '%s'", st.T.Name, s.Name)
				}
				event.WriteString(replacement)
				if st.T.Group > 0 {
					choices[st.T.Group] = choice
				}
			}
		}
		ret[k] = event.String()
		bp.Put(event)
	}
	return ret
}

func genMultiPass(item *config.GenQueueItem) error {
	s := item.S
	slen := len(s.Lines)

	if slen > 0 {
		var events []map[string]string
		events = make([]map[string]string, 0, item.Count)
		if s.RandomizeEvents {
			// log.Debugf("Random filling events for sample '%s' with %d events", s.Name, item.Count)

			for i := 0; i < item.Count; i++ {
				events = append(events, copyevent(s.Lines[item.Rand.Intn(slen)]))
			}
		} else {
			if item.Count <= slen {
				for i := 0; i < item.Count; i++ {
					events = append(events, copyevent(s.Lines[i]))
				}
			} else {
				iters := int(math.Ceil(float64(item.S.Count) / float64(slen)))
				// log.Debugf("Sequentially filling events for sample '%s' of size %d with %d events over %d iterations", s.Name, slen, item.Count, iters)
				for i := 0; i < iters; i++ {
					var count int
					// start := i * slen
					if i == iters-1 {
						count = (item.S.Count - (i * slen))
					} else {
						count = slen
					}
					// log.Debugf("Appending %d events from lines, length %d", count, slen)
					// end := (i * slen) + count
					for j := 0; j < count; j++ {
						events = append(events, copyevent(s.Lines[j]))
					}
				}
			}
		}

		// log.Debugf("Events: %#v", events)

		for i := 0; i < item.Count; i++ {
			choices := make(map[int]*int)
			for _, token := range s.Tokens {
				if fieldval, ok := events[i][token.Field]; ok {
					var choice *int
					if choices[token.Group] != nil {
						choice = choices[token.Group]
					} else {
						choice = new(int)
						*choice = -1
					}
					// log.Debugf("Replacing token '%s':'%s' with choice %d in fieldval: %s", token.Name, token.Token, *choice, fieldval)
					if err := token.Replace(&fieldval, choice, item.Earliest, item.Latest, item.Rand); err == nil {
						events[i][token.Field] = fieldval
					} else {
						log.Error(err)
					}
					if token.Group > 0 {
						choices[token.Group] = choice
					}
				} else {
					log.Errorf("Field %s not found in event for sample %s", token.Field, s.Name)
				}
			}
		}

		outitem := &config.OutQueueItem{S: item.S, Events: events}
		item.OQ <- outitem
	}
	return nil
}

func copyevent(src map[string]string) (dst map[string]string) {
	dst = make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
