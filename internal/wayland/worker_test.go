package wayland

import (
	"errors"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/effect"
	"golang.org/x/sys/unix"
)

func TestArtworkElapsedWorker(t *testing.T) {
	e, err := effect.New("great-wave", "kanagawa", 80, 24, "")
	if err != nil {
		t.Fatal(err)
	}
	w := newEffectWorker(e, "great-wave", "kanagawa", func() {})
	defer w.close()
	_ = receiveFrame(t, w.frames)
	if !w.submit(workerRequest{op: workerTick, elapsed: 120 * time.Millisecond}) {
		t.Fatal("tick rejected")
	}
	_ = receiveAck(t, w.acks)
	got := receiveFrame(t, w.frames)
	want, _ := effect.New("great-wave", "kanagawa", 80, 24, "")
	want.Tick()
	want.TickElapsed(120 * time.Millisecond)
	if !reflect.DeepEqual(got.grid, want.Grid()) {
		t.Fatal("worker dropped elapsed time")
	}
}

func TestArtworkElapsedOwnerPause(t *testing.T) {
	w := &effectWorker{requests: make(chan workerRequest, 2), frames: make(chan workerResult, 1), acks: make(chan workerAck, 2), stop: make(chan struct{})}
	baseline := time.Unix(1000, 0)
	o := &Owner{worker: w, lastTick: baseline, pending: make(map[chan workerAck]chan ControlReply)}
	for _, paused := range []bool{true, false} {
		reply := make(chan workerAck, 1)
		o.pending[reply] = nil
		w.acks <- workerAck{op: workerPause, paused: paused, reply: reply}
		if err := o.drainWorker(); err != nil {
			t.Fatal(err)
		}
		if !o.lastTick.IsZero() {
			t.Fatal("pause transition retained elapsed-time baseline")
		}
	}
	o.onFrame(baseline.Add(time.Hour))
	select {
	case req := <-w.requests:
		if req.elapsed != 50*time.Millisecond {
			t.Fatalf("resumed tick includes paused time: %s", req.elapsed)
		}
	default:
		t.Fatal("resume did not request a tick")
	}
}

func TestArtworkWorkerRetainsCellAspect(t *testing.T) {
	e, err := effect.New("great-wave", "kanagawa", 80, 24, "")
	if err != nil {
		t.Fatal(err)
	}
	w := newEffectWorker(e, "great-wave", "kanagawa", func() {})
	defer w.close()
	_ = receiveFrame(t, w.frames)
	for _, req := range []workerRequest{
		{op: workerResize, cols: 100, rows: 30, cellW: 7, cellH: 16},
		{op: workerChange, id: "ak-47-rama", theme: "dracula"},
		{op: workerReset},
	} {
		req.reply = make(chan workerAck, 1)
		if !w.submit(req) {
			t.Fatal("request rejected")
		}
		ack := receiveAck(t, w.acks)
		if ack.err != nil {
			t.Fatal(ack.err)
		}
		got := receiveFrame(t, w.frames)
		want, err := effect.New(got.id, got.theme, 100, 30, "")
		if err != nil {
			t.Fatal(err)
		}
		want.SetCellAspect(7, 16)
		if err = want.Tick(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.grid, want.Grid()) {
			t.Fatalf("operation %d lost cell aspect", req.op)
		}
	}
}

func TestEffectWorkerTicksAndHonorsPause(t *testing.T) {
	e, err := effect.New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new effect: %v", err)
	}
	w := newEffectWorker(e, "fire", "nord", func() {})
	defer w.close()

	first := receiveFrame(t, w.frames)
	if first.grid == nil || first.generation != 1 {
		t.Fatalf("initial frame = %+v", first)
	}

	pausedReply := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerPause, paused: true, reply: pausedReply}) {
		t.Fatal("pause request was rejected")
	}
	paused := receiveAck(t, w.acks)
	if !paused.paused || paused.reply != pausedReply {
		t.Fatalf("pause result = %+v", paused)
	}

	tickReply := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerTick, reply: tickReply}) {
		t.Fatal("tick request was rejected")
	}
	if tick := receiveAck(t, w.acks); tick.err != nil || !tick.paused {
		t.Fatalf("paused tick result = %+v", tick)
	}
	select {
	case frame := <-w.frames:
		t.Fatalf("paused worker published a frame: %+v", frame)
	default:
	}
}

func TestEffectWorkerChangeAndResize(t *testing.T) {
	e, err := effect.New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new effect: %v", err)
	}
	w := newEffectWorker(e, "fire", "nord", func() {})
	defer w.close()
	_ = receiveFrame(t, w.frames)

	change := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerChange, id: "rain", theme: "dracula", reply: change}) {
		t.Fatal("change request was rejected")
	}
	ack := receiveAck(t, w.acks)
	if ack.err != nil || ack.id != "rain" || ack.theme != "dracula" {
		t.Fatalf("change result = %+v", ack)
	}
	frame := receiveFrame(t, w.frames)
	if frame.id != "rain" || frame.theme != "dracula" || frame.grid == nil {
		t.Fatalf("changed frame = %+v", frame)
	}

	resize := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerResize, cols: 100, rows: 30, reply: resize}) {
		t.Fatal("resize request was rejected")
	}
	ack = receiveAck(t, w.acks)
	if ack.err != nil {
		t.Fatalf("resize result = %+v", ack)
	}
	frame = receiveFrame(t, w.frames)
	if frame.grid == nil || frame.grid.Cols != 100 || frame.grid.Rows != 30 {
		t.Fatalf("resized frame grid = %+v", frame.grid)
	}
}

func TestEffectWorkerResizeWhilePausedPublishesFrame(t *testing.T) {
	e, err := effect.New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new effect: %v", err)
	}
	w := newEffectWorker(e, "fire", "nord", func() {})
	defer w.close()
	_ = receiveFrame(t, w.frames)

	pausedReply := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerPause, paused: true, reply: pausedReply}) {
		t.Fatal("pause request was rejected")
	}
	if paused := receiveAck(t, w.acks); paused.err != nil {
		t.Fatalf("pause result = %+v", paused)
	}

	resize := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerResize, cols: 100, rows: 30, reply: resize}) {
		t.Fatal("resize request was rejected")
	}
	if ack := receiveAck(t, w.acks); ack.err != nil || !ack.paused {
		t.Fatalf("paused resize result = %+v", ack)
	}
	frame := receiveFrame(t, w.frames)
	if frame.grid == nil || frame.grid.Cols != 100 || frame.grid.Rows != 30 {
		t.Fatalf("paused resized frame grid = %+v", frame.grid)
	}

	tickReply := make(chan workerAck, 1)
	if !w.submit(workerRequest{op: workerTick, reply: tickReply}) {
		t.Fatal("tick request was rejected")
	}
	if tick := receiveAck(t, w.acks); tick.err != nil {
		t.Fatalf("paused tick result = %+v", tick)
	}
	select {
	case frame := <-w.frames:
		t.Fatalf("paused worker published a second frame: %+v", frame)
	default:
	}
}

func TestOwnerWakeDoesNotWriteToReusedDescriptorAfterFinish(t *testing.T) {
	for round := 0; round < 10000; round++ {
		wake := [2]int{}
		if err := unix.Pipe2(wake[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
			t.Fatal(err)
		}
		o := &Owner{done: make(chan struct{}), wakeRead: wake[0], wakeWrite: wake[1]}
		start, stop := make(chan struct{}), make(chan struct{})
		var writers sync.WaitGroup
		for i := 0; i < 8; i++ {
			writers.Add(1)
			go func() {
				defer writers.Done()
				<-start
				for {
					select {
					case <-stop:
						return
					default:
						o.Wake()
						runtime.Gosched()
					}
				}
			}()
		}
		close(start)
		for i := 0; i < 4; i++ {
			runtime.Gosched()
		}
		o.finish()
		reused := [2]int{}
		if err := unix.Pipe2(reused[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			runtime.Gosched()
		}
		close(stop)
		writers.Wait()
		var b [8]byte
		n, err := unix.Read(reused[0], b[:])
		_ = unix.Close(reused[0])
		_ = unix.Close(reused[1])
		if n > 0 || (err != nil && !errors.Is(err, unix.EAGAIN)) {
			t.Fatalf("round %d: stale wake wrote %d bytes to a reused descriptor (err %v)", round, n, err)
		}
	}
}

func receiveFrame(t *testing.T, frames <-chan workerResult) workerResult {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for effect worker")
		return workerResult{}
	}
}

func receiveAck(t *testing.T, acks <-chan workerAck) workerAck {
	t.Helper()
	select {
	case ack := <-acks:
		return ack
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for effect worker command")
		return workerAck{}
	}
}
