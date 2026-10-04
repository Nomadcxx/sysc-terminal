package wayland

import (
	"errors"
	"sync"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/Nomadcxx/sysc-terminal/internal/effect"
)

type workerOp uint8

const (
	workerTick workerOp = iota
	workerPause
	workerChange
	workerReset
	workerResize
)

type workerRequest struct {
	op     workerOp
	id     string
	theme  string
	file   string
	cols   int
	rows   int
	paused bool
	reply  chan workerAck
}

type workerResult struct {
	grid       *cell.Grid
	id, theme  string
	generation int
	err        error
}

type workerAck struct {
	op        workerOp
	id, theme string
	paused    bool
	err       error
	reply     chan workerAck
}

type effectWorker struct {
	requests  chan workerRequest
	frames    chan workerResult
	acks      chan workerAck
	stop      chan struct{}
	done      chan struct{}
	wake      func()
	closeOnce sync.Once
}

func newEffectWorker(initial *effect.Effect, id, theme string, wake func()) *effectWorker {
	w := &effectWorker{
		requests: make(chan workerRequest, 16),
		frames:   make(chan workerResult, 1),
		acks:     make(chan workerAck, 16),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		wake:     wake,
	}
	go w.run(initial, id, theme)
	return w
}

func (w *effectWorker) submit(req workerRequest) bool {
	select {
	case <-w.stop:
		return false
	case w.requests <- req:
		return true
	default:
		return false
	}
}

func (w *effectWorker) close() {
	w.closeOnce.Do(func() { close(w.stop) })
	<-w.done
}

func (w *effectWorker) run(current *effect.Effect, id, theme string) {
	defer close(w.done)
	paused := false
	if err := current.Tick(); err != nil {
		w.publishFrame(workerResult{err: err})
		return
	}
	w.publishFrame(w.snapshot(current, id, theme, nil))
	for {
		select {
		case <-w.stop:
			return
		case req := <-w.requests:
			var err error
			publish := false
			switch req.op {
			case workerTick:
				if !paused {
					err = current.Tick()
					publish = true
				}
			case workerPause:
				paused = req.paused
				current.SetPaused(paused)
			case workerChange:
				var next *effect.Effect
				if req.file != "" {
					next, err = effect.NewFromFile(req.id, req.theme, current.EffectWidth(), current.EffectHeight(), req.file)
				} else {
					next, err = effect.New(req.id, req.theme, current.EffectWidth(), current.EffectHeight(), "")
				}
				if err == nil {
					next.SetPaused(paused)
					if !paused {
						err = next.Tick()
					}
				}
				if err == nil {
					current, id, theme = next, req.id, req.theme
					publish = !paused
				}
			case workerReset:
				err = current.Reset()
				if err == nil && !paused {
					err = current.Tick()
					publish = true
				}
			case workerResize:
				err = current.Resize(req.cols, req.rows)
				if err == nil && !paused {
					err = current.Tick()
					publish = true
				}
			default:
				err = errors.New("unknown effect worker request")
			}

			if publish {
				w.publishFrame(w.snapshot(current, id, theme, err))
				if err != nil {
					return
				}
			}
			if req.reply != nil || req.op == workerTick {
				ack := workerAck{op: req.op, id: id, theme: theme, paused: paused, err: err, reply: req.reply}
				select {
				case w.acks <- ack:
					if w.wake != nil {
						w.wake()
					}
				case <-w.stop:
					return
				}
			}
		}
	}
}

func (w *effectWorker) snapshot(e *effect.Effect, id, theme string, err error) workerResult {
	return workerResult{grid: e.Grid(), id: id, theme: theme, generation: e.Generation(), err: err}
}

func (w *effectWorker) publishFrame(frame workerResult) {
	select {
	case w.frames <- frame:
	default:
		select {
		case <-w.frames:
		default:
		}
		select {
		case w.frames <- frame:
		default:
		}
	}
	if w.wake != nil {
		w.wake()
	}
}
