package wayland

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/effect"
	"golang.org/x/sys/unix"
)

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
